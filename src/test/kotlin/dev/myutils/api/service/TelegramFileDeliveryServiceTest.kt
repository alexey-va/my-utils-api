package dev.myutils.api.service

import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.telegram.TelegramMessenger
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.eq
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever
import org.springframework.beans.factory.ObjectProvider
import org.springframework.mock.web.MockMultipartFile

class TelegramFileDeliveryServiceTest {
	private val telegram: TelegramMessenger = mock()
	private val telegramProvider: ObjectProvider<TelegramMessenger> = mock()

	@Test
	fun `accepts token derived from configured Telegram bot token`() {
		val properties =
			MyUtilsProperties(
				telegram =
					MyUtilsProperties.TelegramProperties(
						enabled = true,
						botToken = "bot-secret",
						allowedUserIds = "999",
					),
			)
		val service = TelegramFileDeliveryService(properties, telegramProvider)
		val file = MockMultipartFile("file", "report.txt", "text/plain", "hello".toByteArray())
		whenever(telegramProvider.getIfAvailable()).thenReturn(telegram)
		whenever(
			telegram.sendDocument(
				chatId = eq(999),
				bytes = any(),
				fileName = eq("report.txt"),
				contentType = eq("text/plain"),
				caption = eq(null),
			),
		).thenReturn(true)

		val response =
			service.deliver(
				providedToken = "4984054325ef99682d6a9580018f602e1fca016ff1e6070c339e9637eec037b3",
				file = file,
				caption = null,
			)

		assertEquals(1, response.sentTo)
		verify(telegram).sendDocument(
			chatId = eq(999),
			bytes = any(),
			fileName = eq("report.txt"),
			contentType = eq("text/plain"),
			caption = eq(null),
		)
	}
}
