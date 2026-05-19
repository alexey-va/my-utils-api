package dev.myutils.api.telegram

import dev.myutils.api.config.MyUtilsProperties
import org.slf4j.LoggerFactory
import dev.myutils.api.config.ConditionalOnTelegramBot
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class TelegramBotBootstrap(
	private val properties: MyUtilsProperties,
	private val telegramClient: TelegramClient,
	private val longPollingRunner: TelegramLongPollingRunner,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@EventListener(ApplicationReadyEvent::class)
	fun onReady() {
		val telegram = properties.telegram

		if (longPollingRunner.shouldUsePolling()) {
			return
		}

		val base = telegram.webhookBaseUrl.trimEnd('/')
		val secret = telegram.webhookSecret
		if (secret.isBlank()) {
			log.warn("Telegram webhook-secret is empty (required for webhook mode)")
			return
		}
		telegramClient.setWebhook("$base/api/telegram/webhook/$secret")
	}
}
