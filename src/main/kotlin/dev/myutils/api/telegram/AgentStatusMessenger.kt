package dev.myutils.api.telegram

import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.stereotype.Component
import java.time.Duration

/** Одно статус-сообщение в Telegram — всегда только текущее действие. */
@Component
@ConditionalOnTelegramBot
class AgentStatusMessenger(
	private val telegram: TelegramMessenger,
	private val redis: StringRedisTemplate,
) {
	fun begin(chatId: Long) {
		reset(chatId)
		show(chatId, AgentStatusLabels.thinking())
	}

	fun thinking(
		chatId: Long,
		step: Int,
	) {
		show(chatId, AgentStatusLabels.thinking(step))
	}

	fun toolsStarted(
		chatId: Long,
		toolNames: List<String>,
	) {
		show(chatId, AgentStatusLabels.toolsRunning(toolNames))
	}

	fun composingReply(chatId: Long) {
		show(chatId, AgentStatusLabels.COMPOSING_REPLY)
	}

	fun complete(chatId: Long) {
		val messageId = loadMessageId(chatId) ?: return
		telegram.deleteMessage(chatId, messageId)
		clear(chatId)
	}

	fun fail(
		chatId: Long,
		text: String,
	) {
		update(chatId, "❌ $text")
		clear(chatId)
	}

	private fun show(
		chatId: Long,
		text: String,
	) {
		update(chatId, "⏳ $text")
	}

	private fun update(
		chatId: Long,
		text: String,
	) {
		val messageId = loadMessageId(chatId)
		if (messageId == null) {
			val sentId = telegram.sendHtmlMessage(chatId, text)
			if (sentId != null) {
				storeMessageId(chatId, sentId)
			}
		} else {
			telegram.editHtmlMessage(chatId, messageId, text)
		}
		telegram.sendTyping(chatId)
	}

	private fun reset(chatId: Long) {
		loadMessageId(chatId)?.let { telegram.deleteMessage(chatId, it) }
		clear(chatId)
	}

	private fun storeMessageId(
		chatId: Long,
		messageId: Int,
	) {
		redis.opsForValue().set(key(chatId), messageId.toString(), STATUS_TTL)
	}

	private fun loadMessageId(chatId: Long): Int? = redis.opsForValue().get(key(chatId))?.toIntOrNull()

	private fun clear(chatId: Long) {
		redis.delete(key(chatId))
	}

	private fun key(chatId: Long): String = "agent:status:$chatId"

	companion object {
		private val STATUS_TTL: Duration = Duration.ofHours(2)
	}
}
