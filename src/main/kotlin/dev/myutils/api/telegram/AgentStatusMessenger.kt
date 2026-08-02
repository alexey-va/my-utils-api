package dev.myutils.api.telegram

import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import org.slf4j.LoggerFactory
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
	private val log = LoggerFactory.getLogger(javaClass)

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

	fun toolRunning(
		chatId: Long,
		toolName: String,
	) {
		show(chatId, AgentStatusLabels.toolRunning(toolName))
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
		try {
			loadMessageId(chatId)?.let { telegram.deleteMessage(chatId, it) }
		} catch (cleanupError: Exception) {
			log.warn("Failed to remove agent status before terminal error chatId={}", chatId, cleanupError)
		}
		try {
			clear(chatId)
		} catch (cleanupError: Exception) {
			log.warn("Failed to clear agent status before terminal error chatId={}", chatId, cleanupError)
		}
		val terminalText = if (text.startsWith("❌")) text else "❌ $text"
		telegram.sendHtmlMessage(chatId, terminalText)
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
