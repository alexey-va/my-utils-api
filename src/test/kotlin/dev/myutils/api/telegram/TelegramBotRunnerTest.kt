package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.model.Chat
import com.pengrad.telegrambot.model.Message
import com.pengrad.telegrambot.model.User
import dev.myutils.api.infra.config.MyUtilsProperties
import org.junit.jupiter.api.Test
import org.mockito.kotlin.eq
import org.mockito.kotlin.isNull
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.verifyNoInteractions
import org.mockito.kotlin.whenever

class TelegramBotRunnerTest {
	@Test
	fun `non text message gets explicit terminal reply`() {
		val user = mock<User>()
		whenever(user.id()).thenReturn(7L)
		val chat = mock<Chat>()
		whenever(chat.id()).thenReturn(42L)
		val message = mock<Message>()
		whenever(message.from()).thenReturn(user)
		whenever(message.chat()).thenReturn(chat)
		whenever(message.text()).thenReturn(null)
		val messenger = mock<TelegramMessenger>()
		val coalescer = mock<TelegramInboundCoalescer>()
		val runner =
			TelegramBotRunner(
				properties = MyUtilsProperties(),
				bot = mock<TelegramBot>(),
				messenger = messenger,
				inboundCoalescer = coalescer,
			)

		runner.routeMessage(message)

		verify(messenger).sendHtmlMessage(
			eq(42L),
			eq("❌ Я понимаю только текстовые сообщения."),
			isNull(),
		)
		verifyNoInteractions(coalescer)
	}
}
