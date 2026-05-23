package dev.myutils.api.telegram

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.telegram.TelegramApiSupport.describeError
import dev.myutils.api.telegram.TelegramApiSupport.timed
import dev.myutils.api.util.LogPreview
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.slf4j.LoggerFactory
import org.springframework.http.client.JdkClientHttpRequestFactory
import org.springframework.stereotype.Component
import org.springframework.web.client.RestClient
import java.net.http.HttpClient
import java.time.Duration

@Component
@ConditionalOnTelegramBot
class TelegramClient(
	properties: MyUtilsProperties,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val config = properties.telegram

	private val client: RestClient = createRestClient()

	private fun createRestClient(): RestClient {
		val httpClient =
			HttpClient
				.newBuilder()
				.connectTimeout(Duration.ofSeconds(15))
				.build()
		val requestFactory =
			JdkClientHttpRequestFactory(httpClient).apply {
				setReadTimeout(Duration.ofSeconds(45))
			}
		return RestClient
			.builder()
			.baseUrl("https://api.telegram.org/bot${config.botToken}")
			.requestFactory(requestFactory)
			.build()
	}

	suspend fun sendMessage(
		chatId: Long,
		text: String,
	) = withContext(Dispatchers.IO) {
		log.timed("sendMessage chatId=$chatId chars=${text.length}") {
			val response =
				client
					.post()
					.uri("/sendMessage")
					.body(SendMessageRequest(chatId = chatId, text = text.take(4096)))
					.retrieve()
					.body(TelegramApiResponse::class.java)
			if (response?.ok == true) {
				log.info(
					"Telegram sendMessage ok chatId={} text={}",
					chatId,
					LogPreview.of(text),
				)
			} else {
				log.warn("Telegram sendMessage failed chatId={}: {}", chatId, response?.description)
			}
		}
	}

	suspend fun sendChatAction(
		chatId: Long,
		action: String,
	) = withContext(Dispatchers.IO) {
		runCatching {
			log.timed("sendChatAction chatId=$chatId action=$action") {
				client
					.post()
					.uri("/sendChatAction")
					.body(SendChatActionRequest(chatId = chatId, action = action))
					.retrieve()
					.toBodilessEntity()
			}
		}.onFailure { ex ->
			log.debug("Telegram sendChatAction failed chatId={}: {}", chatId, describeError(ex), ex)
		}
	}

	suspend fun ensureLongPollingMode() =
		withContext(Dispatchers.IO) {
			log.info("Telegram ensuring long polling mode")
			val before = getWebhookInfo()
			if (before == null) {
				log.warn("Telegram getWebhookInfo failed before deleteWebhook — continuing anyway")
			} else {
				log.info(
					"Telegram webhook before delete: url={} pendingUpdates={} customCert={}",
					before.url?.ifBlank { "<empty>" } ?: "<empty>",
					before.pendingUpdateCount,
					before.hasCustomCertificate,
				)
			}

			runCatching {
				log.timed("deleteWebhook") {
					val response =
						client
							.post()
							.uri("/deleteWebhook")
							.body(mapOf("drop_pending_updates" to false))
							.retrieve()
							.body(TelegramApiResponse::class.java)
					if (response?.ok == true) {
						log.info("Telegram deleteWebhook ok")
					} else {
						log.warn("Telegram deleteWebhook api error: {}", response?.description)
					}
					response
				}
			}.onFailure { ex ->
				log.warn("Telegram deleteWebhook failed: {}", describeError(ex), ex)
			}

			val after = getWebhookInfo()
			if (after == null) {
				log.warn("Telegram getWebhookInfo failed after deleteWebhook")
			} else {
				log.info(
					"Telegram webhook after delete: url={} pendingUpdates={}",
					after.url?.ifBlank { "<empty>" } ?: "<empty>",
					after.pendingUpdateCount,
				)
			}
		}

	suspend fun getUpdates(
		offset: Long,
		timeoutSeconds: Int = 30,
	): List<TelegramUpdate> =
		withContext(Dispatchers.IO) {
			log.timed("getUpdates offset=$offset timeout=${timeoutSeconds}s") {
				val response =
					client
						.get()
						.uri { builder ->
							builder
								.path("/getUpdates")
								.queryParam("offset", offset)
								.queryParam("timeout", timeoutSeconds)
								.queryParam("allowed_updates", "[\"message\",\"edited_message\"]")
								.build()
						}
						.retrieve()
						.body(TelegramUpdatesResult::class.java)
				if (response?.ok != true) {
					log.warn("Telegram getUpdates api error: {}", response?.description)
					return@timed emptyList()
				}
				if (response.result.isNotEmpty()) {
					log.info("Telegram getUpdates returned {} update(s)", response.result.size)
				}
				response.result
			}
		}

	private fun getWebhookInfo(): TelegramWebhookInfo? =
		log.timed("getWebhookInfo") {
			val response =
				client
					.get()
					.uri("/getWebhookInfo")
					.retrieve()
					.body(TelegramWebhookInfoResponse::class.java)
			if (response?.ok != true) {
				log.warn("Telegram getWebhookInfo api error: {}", response?.description)
				return@timed null
			}
			response.result
		}
}
