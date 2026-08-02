package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.request.EditMessageText
import com.pengrad.telegrambot.response.BaseResponse
import org.junit.jupiter.api.Assertions.assertDoesNotThrow
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.whenever

class PengradTelegramMessengerTest {
	@Test
	fun `identical status edit is treated as successful idempotent update`() {
		val bot = mock<TelegramBot>()
		val response = mock<BaseResponse>()
		whenever(response.isOk).thenReturn(false)
		whenever(response.errorCode()).thenReturn(400)
		whenever(response.description()).thenReturn(
			"Bad Request: message is not modified: specified new message content is exactly the same",
		)
		whenever(bot.execute(any<EditMessageText>())).thenReturn(response)
		val messenger = PengradTelegramMessenger(bot)

		assertDoesNotThrow {
			messenger.editHtmlMessage(chatId = 42L, messageId = 100, text = "⏳ Думаю…")
		}
	}
}
