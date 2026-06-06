package dev.myutils.api.telegram

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.infra.openrouter.ChatMessage
import org.slf4j.LoggerFactory
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.stereotype.Component
import java.time.Duration

@Component
@ConditionalOnTelegramBot
class TelegramChatHistory(
	private val redis: StringRedisTemplate,
	private val properties: MyUtilsProperties,
	private val objectMapper: ObjectMapper,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val config = properties.telegram
	private val maxMessages = 24

	fun load(chatId: Long): List<ChatMessage> {
		val raw = redis.opsForValue().get(key(chatId)) ?: return emptyList()
		val messages = runCatching { objectMapper.readValue<List<ChatMessage>>(raw) }.getOrDefault(emptyList())
		log.debug("Redis history load chatId={} messages={}", chatId, messages.size)
		return messages
	}

	fun save(
		chatId: Long,
		messages: List<ChatMessage>,
	) {
		val trimmed = messages.takeLast(maxMessages)
		log.debug("Redis history save chatId={} messages={}", chatId, trimmed.size)
		redis.opsForValue().set(
			key(chatId),
			objectMapper.writeValueAsString(trimmed),
			Duration.ofHours(AppProperties.TELEGRAM_CONVERSATION_TTL_HOURS.get().toLong()),
		)
	}

	private fun key(chatId: Long): String = "${config.conversationKeyPrefix}$chatId"
}
