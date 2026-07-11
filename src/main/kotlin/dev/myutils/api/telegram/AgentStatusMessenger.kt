package dev.myutils.api.telegram

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.stereotype.Component
import java.time.Duration

/**
 * Накопительный статус в одном Telegram-сообщении (как progress в Cursor).
 * Строки не исчезают, пока идут тулы и пока LLM формирует ответ.
 */
@Component
@ConditionalOnTelegramBot
class AgentStatusMessenger(
	private val telegram: TelegramMessenger,
	private val redis: StringRedisTemplate,
	private val objectMapper: ObjectMapper,
) {
	fun begin(chatId: Long) {
		reset(chatId)
		val state =
			AgentStatusState(
				messageId = null,
				lines = mutableListOf(StatusLine.pending(AgentStatusLabels.thinking())),
			)
		flush(chatId, state)
	}

	fun thinking(
		chatId: Long,
		step: Int,
	) {
		mutate(chatId) { state ->
			state.completeActive()
			state.lines.add(StatusLine.pending(AgentStatusLabels.thinking(step)))
		}
	}

	fun toolsStarted(
		chatId: Long,
		toolNames: List<String>,
	) {
		if (toolNames.isEmpty()) {
			return
		}
		mutate(chatId) { state ->
			state.completeActive()
			for (label in AgentStatusLabels.toolsRunning(toolNames)) {
				state.lines.add(StatusLine.pending(label))
			}
			state.trimTail()
		}
	}

	fun toolsFinished(chatId: Long) {
		mutate(chatId) { state ->
			state.completeActive()
		}
	}

	fun composingReply(chatId: Long) {
		mutate(chatId) { state ->
			state.completeActive()
			state.lines.add(StatusLine.pending(AgentStatusLabels.COMPOSING_REPLY))
			state.trimTail()
		}
	}

	fun complete(chatId: Long) {
		val state = load(chatId) ?: return
		state.messageId?.let { telegram.deleteMessage(chatId, it) }
		clear(chatId)
	}

	fun fail(
		chatId: Long,
		text: String,
	) {
		mutate(chatId) { state ->
			state.completeActive()
			state.lines.add(StatusLine.failed(text))
			state.trimTail()
		}
		clear(chatId)
	}

	private fun mutate(
		chatId: Long,
		block: (AgentStatusState) -> Unit,
	) {
		val state = load(chatId) ?: AgentStatusState(messageId = null, lines = mutableListOf())
		block(state)
		flush(chatId, state)
	}

	private fun flush(
		chatId: Long,
		state: AgentStatusState,
	) {
		val body = state.render()
		val messageId = state.messageId
		if (messageId == null) {
			val sentId = telegram.sendHtmlMessage(chatId, body)
			if (sentId != null) {
				state.messageId = sentId
				save(chatId, state)
			}
		} else {
			telegram.editHtmlMessage(chatId, messageId, body)
			save(chatId, state)
		}
		telegram.sendTyping(chatId)
	}

	private fun reset(chatId: Long) {
		val state = load(chatId)
		state?.messageId?.let { telegram.deleteMessage(chatId, it) }
		clear(chatId)
	}

	private fun save(
		chatId: Long,
		state: AgentStatusState,
	) {
		redis.opsForValue().set(key(chatId), objectMapper.writeValueAsString(state), STATUS_TTL)
	}

	private fun load(chatId: Long): AgentStatusState? {
		val raw = redis.opsForValue().get(key(chatId)) ?: return null
		return runCatching { objectMapper.readValue<AgentStatusState>(raw) }.getOrNull()
	}

	private fun clear(chatId: Long) {
		redis.delete(key(chatId))
	}

	private fun key(chatId: Long): String = "agent:status:$chatId"

	companion object {
		private val STATUS_TTL: Duration = Duration.ofHours(2)
		const val MAX_VISIBLE_LINES: Int = 12
	}
}

private data class AgentStatusState(
	var messageId: Int?,
	val lines: MutableList<StatusLine>,
) {
	fun completeActive() {
		for (line in lines) {
			if (line.pending) {
				line.markDone()
			}
		}
	}

	fun render(): String = lines.joinToString("\n") { it.render() }

	fun trimTail(maxLines: Int = AgentStatusMessenger.MAX_VISIBLE_LINES) {
		while (lines.size > maxLines) {
			lines.removeAt(0)
		}
	}
}

private data class StatusLine(
	var emoji: String,
	val text: String,
	var pending: Boolean,
) {
	fun markDone() {
		emoji = "✓"
		pending = false
	}

	fun render(): String = "$emoji $text"

	companion object {
		fun pending(text: String): StatusLine = StatusLine(emoji = "⏳", text = text, pending = true)

		fun failed(text: String): StatusLine = StatusLine(emoji = "❌", text = text, pending = false)
	}
}
