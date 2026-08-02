package dev.myutils.api.telegram

import com.pengrad.telegrambot.model.request.Keyboard
import dev.myutils.api.agent.WorkoutAgentService
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.doAnswer
import org.mockito.kotlin.mock
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.CopyOnWriteArrayList

class TelegramInboundCoalescerTest {
	@Test
	fun `sends terminal error reply when message handling escapes exceptionally`() {
		val attempted = CountDownLatch(1)
		val agent: WorkoutAgentService =
			mock {
				onBlocking { handleMessage(any(), any(), any()) } doAnswer {
					throw IllegalStateException("Temporal unavailable")
				}
			}
		val terminalText = AtomicReference<String>()
		val telegram =
			object : TelegramMessenger {
				override fun sendHtmlMessage(
					chatId: Long,
					text: String,
					replyMarkup: Keyboard?,
				): Int {
					terminalText.set(text)
					attempted.countDown()
					return 1
				}

				override fun editHtmlMessage(chatId: Long, messageId: Int, text: String) = Unit

				override fun deleteMessage(chatId: Long, messageId: Int) = Unit

				override fun sendTyping(chatId: Long) = Unit

				override fun sendPhoto(chatId: Long, png: ByteArray, caption: String?) = Unit

				override fun answerCallback(callbackQueryId: String, text: String?) = Unit
			}
		val coalescer = TelegramInboundCoalescer(agent, telegram)

		try {
			coalescer.enqueue(chatId = 42L, userId = 7L, text = "что сегодня")

			assertTrue(attempted.await(2, TimeUnit.SECONDS))
			assertEquals("❌ Не удалось обработать запрос. Попробуй ещё раз.", terminalText.get())
		} finally {
			coalescer.shutdown()
		}
	}

	@Test
	fun `processes every queued message for the same chat`() {
		val firstEntered = CountDownLatch(1)
		val releaseFirst = CountDownLatch(1)
		val allProcessed = CountDownLatch(3)
		val processed = CopyOnWriteArrayList<String>()
		val agent: WorkoutAgentService =
			mock {
				onBlocking { handleMessage(any(), any(), any()) } doAnswer { invocation ->
					val text = invocation.getArgument<String>(2)
					processed.add(text)
					if (text == "первое") {
						firstEntered.countDown()
						releaseFirst.await(2, TimeUnit.SECONDS)
					}
					allProcessed.countDown()
					Unit
				}
			}
		val telegram = mock<TelegramMessenger>()
		val coalescer = TelegramInboundCoalescer(agent, telegram)

		try {
			coalescer.enqueue(42L, 7L, "первое")
			assertTrue(firstEntered.await(2, TimeUnit.SECONDS))
			coalescer.enqueue(42L, 7L, "второе")
			coalescer.enqueue(42L, 7L, "третье")
			releaseFirst.countDown()

			assertTrue(allProcessed.await(2, TimeUnit.SECONDS))
			assertEquals(listOf("первое", "второе", "третье"), processed)
		} finally {
			coalescer.shutdown()
		}
	}
}
