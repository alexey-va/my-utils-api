package dev.myutils.api.telegram

import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.stereotype.Component
import java.time.Duration

/**
 * Постоянное статус-сообщение в Telegram на время обработки агентом.
 * Typing исчезает через ~5 с — статус остаётся и обновляется.
 */
@Component
@ConditionalOnTelegramBot
class AgentStatusMessenger(
	private val telegram: TelegramMessenger,
	private val redis: StringRedisTemplate,
) {
	fun begin(
		chatId: Long,
		text: String = "⏳ Обрабатываю…",
	) {
		clear(chatId)
		val messageId = telegram.sendHtmlMessage(chatId, text)
		if (messageId != null) {
			store(chatId, messageId)
		}
		telegram.sendTyping(chatId)
	}

	fun update(
		chatId: Long,
		text: String,
	) {
		val messageId = load(chatId)
		if (messageId != null) {
			telegram.editHtmlMessage(chatId, messageId, text)
		} else {
			begin(chatId, text)
			return
		}
		telegram.sendTyping(chatId)
	}

	fun complete(chatId: Long) {
		val messageId = load(chatId)
		if (messageId != null) {
			telegram.deleteMessage(chatId, messageId)
		}
		clear(chatId)
	}

	fun fail(
		chatId: Long,
		text: String,
	) {
		val messageId = load(chatId)
		if (messageId != null) {
			telegram.editHtmlMessage(chatId, messageId, text)
		} else {
			telegram.sendHtmlMessage(chatId, text)
		}
		clear(chatId)
	}

	private fun store(
		chatId: Long,
		messageId: Int,
	) {
		redis.opsForValue().set(key(chatId), messageId.toString(), STATUS_TTL)
	}

	private fun load(chatId: Long): Int? = redis.opsForValue().get(key(chatId))?.toIntOrNull()

	private fun clear(chatId: Long) {
		redis.delete(key(chatId))
	}

	private fun key(chatId: Long): String = "agent:status:$chatId"

	companion object {
		private val STATUS_TTL: Duration = Duration.ofHours(2)
	}
}
