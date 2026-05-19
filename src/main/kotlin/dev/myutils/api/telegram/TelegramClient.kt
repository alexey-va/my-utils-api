package dev.myutils.api.telegram

import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory
import dev.myutils.api.config.ConditionalOnTelegramBot
import org.springframework.stereotype.Component
import org.springframework.web.client.RestClient

@Component
@ConditionalOnTelegramBot
class TelegramClient(
	properties: MyUtilsProperties,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val config = properties.telegram

	private val client: RestClient =
		RestClient
			.builder()
			.baseUrl("https://api.telegram.org/bot${config.botToken}")
			.build()

	fun sendMessage(
		chatId: Long,
		text: String,
	) {
		val response =
			client
				.post()
				.uri("/sendMessage")
				.body(SendMessageRequest(chatId = chatId, text = text.take(4096)))
				.retrieve()
				.body(TelegramApiResponse::class.java)
		if (response?.ok == true) {
			log.info(
				"Telegram sendMessage ok chatId={} chars={} text={}",
				chatId,
				text.length,
				LogPreview.of(text),
			)
		} else {
			log.warn("Telegram sendMessage failed chatId={}: {}", chatId, response?.description)
		}
	}

	fun sendChatAction(
		chatId: Long,
		action: String,
	) {
		runCatching {
			client
				.post()
				.uri("/sendChatAction")
				.body(SendChatActionRequest(chatId = chatId, action = action))
				.retrieve()
				.toBodilessEntity()
		}.onFailure { log.debug("sendChatAction failed", it) }
	}

	fun setWebhook(publicUrl: String) {
		val response =
			client
				.post()
				.uri("/setWebhook")
				.body(
					SetWebhookRequest(
						url = publicUrl,
					),
				)
				.retrieve()
				.body(TelegramApiResponse::class.java)
		if (response?.ok == true) {
			log.info("Telegram webhook set to {}", publicUrl)
		} else {
			log.error("setWebhook failed: {}", response?.description)
		}
	}

	/** Required before long polling — Telegram allows only one update mode. */
	fun deleteWebhook() {
		val response =
			client
				.post()
				.uri("/deleteWebhook")
				.retrieve()
				.body(TelegramApiResponse::class.java)
		if (response?.ok == true) {
			log.info("Telegram webhook removed (long polling mode)")
		} else {
			log.warn("deleteWebhook failed: {}", response?.description)
		}
	}

	fun getUpdates(
		offset: Long,
		timeoutSeconds: Int = 30,
	): List<TelegramUpdate> {
		val response =
			client
				.get()
				.uri { builder ->
					builder
						.path("/getUpdates")
						.queryParam("offset", offset)
						.queryParam("timeout", timeoutSeconds)
						.queryParam("allowed_updates", "message", "edited_message")
						.build()
				}
				.retrieve()
				.body(TelegramUpdatesResult::class.java)
		if (response?.ok != true) {
			log.warn("getUpdates failed: {}", response?.description)
			return emptyList()
		}
		return response.result
	}
}
