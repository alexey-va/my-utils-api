package dev.myutils.api.telegram

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import com.pengrad.telegrambot.model.request.Keyboard
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.data.redis.core.ValueOperations
import java.time.Duration
import java.util.concurrent.ConcurrentHashMap

class AgentStatusMessengerTest {
	private val mapper: ObjectMapper = jacksonObjectMapper()

	@Test
	fun `accumulates tool lines and keeps composing visible`() {
		val telegram = RecordingTelegramMessenger()
		val (redis, store) = inMemoryRedis()
		val messenger = AgentStatusMessenger(telegram, redis, mapper)

		messenger.begin(42L)
		messenger.toolsStarted(42L, listOf("logWorkout", "getDays"))
		messenger.toolsFinished(42L)
		messenger.composingReply(42L)

		val saved = store["agent:status:42"]!!
		val state = mapper.readValue<AgentStatusStateDto>(saved)
		val rendered = state.lines.joinToString("\n") { "${it.emoji} ${it.text}" }
		assertTrue(rendered.contains("✓ Думаю…"))
		assertTrue(rendered.contains("✓ Записываю в дневник…"))
		assertTrue(rendered.contains("✓ Получаю статистику по дням…"))
		assertTrue(rendered.contains("⏳ Формирую ответ…"))
		assertTrue(telegram.edits.last().contains("⏳ Формирую ответ…"))
	}

	@Test
	fun `complete deletes status message`() {
		val telegram = RecordingTelegramMessenger()
		val (redis, store) = inMemoryRedis()
		val messenger = AgentStatusMessenger(telegram, redis, mapper)
		store["agent:status:7"] =
			mapper.writeValueAsString(AgentStatusStateDto(messageId = 55, lines = mutableListOf()))

		messenger.complete(7L)

		assertTrue(telegram.deleted.contains(7L to 55))
	}

	private class RecordingTelegramMessenger : TelegramMessenger {
		var nextMessageId = 100
		val edits = mutableListOf<String>()
		val deleted = mutableListOf<Pair<Long, Int>>()

		override fun sendHtmlMessage(
			chatId: Long,
			text: String,
			replyMarkup: Keyboard?,
		): Int? = nextMessageId++

		override fun editHtmlMessage(
			chatId: Long,
			messageId: Int,
			text: String,
		) {
			edits.add(text)
		}

		override fun deleteMessage(
			chatId: Long,
			messageId: Int,
		) {
			deleted.add(chatId to messageId)
		}

		override fun sendTyping(chatId: Long) = Unit

		override fun answerCallback(
			callbackQueryId: String,
			text: String?,
		) = Unit
	}

	private fun inMemoryRedis(): Pair<StringRedisTemplate, ConcurrentHashMap<String, String>> {
		val template = org.mockito.kotlin.mock<StringRedisTemplate>()
		val valueOps = org.mockito.kotlin.mock<ValueOperations<String, String>>()
		val store = ConcurrentHashMap<String, String>()
		org.mockito.kotlin.whenever(template.opsForValue()).thenReturn(valueOps)
		org.mockito.kotlin.whenever(valueOps.get(org.mockito.kotlin.any<String>())).thenAnswer { inv ->
			store[inv.getArgument(0)]
		}
		org.mockito.kotlin.whenever(
			valueOps.set(
				org.mockito.kotlin.any<String>(),
				org.mockito.kotlin.any<String>(),
				org.mockito.kotlin.any<Duration>(),
			),
		).thenAnswer { inv ->
			store[inv.getArgument(0)] = inv.getArgument(1)
			null
		}
		org.mockito.kotlin.whenever(template.delete(org.mockito.kotlin.any<String>())).thenAnswer { inv ->
			store.remove(inv.getArgument(0))
			true
		}
		return template to store
	}

	private data class AgentStatusStateDto(
		var messageId: Int?,
		val lines: MutableList<StatusLineDto>,
	)

	private data class StatusLineDto(
		var emoji: String,
		val text: String,
		var pending: Boolean,
	)
}
