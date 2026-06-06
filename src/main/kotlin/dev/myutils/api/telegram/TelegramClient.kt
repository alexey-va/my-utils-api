package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.model.request.ChatAction
import com.pengrad.telegrambot.model.request.InlineKeyboardMarkup
import com.pengrad.telegrambot.model.request.Keyboard
import com.pengrad.telegrambot.model.request.ParseMode
import com.pengrad.telegrambot.request.AnswerCallbackQuery
import com.pengrad.telegrambot.request.DeleteWebhook
import com.pengrad.telegrambot.request.GetWebhookInfo
import com.pengrad.telegrambot.request.SendChatAction
import com.pengrad.telegrambot.request.SendMessage
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.telegram.TelegramApiSupport.describeError
import dev.myutils.api.telegram.TelegramApiSupport.timed
import dev.myutils.api.util.LogPreview
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class TelegramClient(
	private val bot: TelegramBot,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	suspend fun sendMessage(
		chatId: Long,
		text: String,
		replyMarkup: Keyboard? = null,
	) = withContext(Dispatchers.IO) {
		log.timed("sendMessage chatId=$chatId chars=${text.length}") {
			val request =
				SendMessage(chatId, text.take(4096))
					.parseMode(ParseMode.HTML)
			if (replyMarkup != null) {
				request.replyMarkup(replyMarkup)
			}
			val response = bot.execute(request)
			if (response.isOk) {
				log.info(
					"Telegram sendMessage ok chatId={} text={}",
					chatId,
					LogPreview.of(text),
				)
			} else {
				log.warn(
					"Telegram sendMessage failed chatId={}: {} {}",
					chatId,
					response.errorCode(),
					response.description(),
				)
			}
		}
	}

	suspend fun sendMessage(
		chatId: Long,
		text: String,
		inlineKeyboard: InlineKeyboardMarkup,
	) = sendMessage(chatId, text, inlineKeyboard as Keyboard)

	suspend fun sendChatAction(
		chatId: Long,
		action: String,
	) = withContext(Dispatchers.IO) {
		runCatching {
			log.timed("sendChatAction chatId=$chatId action=$action") {
				val chatAction = mapChatAction(action)
				val response = bot.execute(SendChatAction(chatId, chatAction))
				if (!response.isOk) {
					log.debug(
						"Telegram sendChatAction api error chatId={}: {} {}",
						chatId,
						response.errorCode(),
						response.description(),
					)
				}
			}
		}.onFailure { ex ->
			log.debug("Telegram sendChatAction failed chatId={}: {}", chatId, describeError(ex), ex)
		}
	}

	suspend fun answerCallbackQuery(
		callbackQueryId: String,
		text: String? = null,
		showAlert: Boolean = false,
	) = withContext(Dispatchers.IO) {
		runCatching {
			log.timed("answerCallbackQuery id=$callbackQueryId") {
				val request = AnswerCallbackQuery(callbackQueryId)
				if (!text.isNullOrBlank()) {
					request.text(text.take(200))
				}
				if (showAlert) {
					request.showAlert(true)
				}
				val response = bot.execute(request)
				if (!response.isOk) {
					log.warn(
						"Telegram answerCallbackQuery failed: {} {}",
						response.errorCode(),
						response.description(),
					)
				}
			}
		}.onFailure { ex ->
			log.warn("Telegram answerCallbackQuery failed: {}", describeError(ex), ex)
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
					"Telegram webhook before delete: url={} pendingUpdates={}",
					before.url()?.ifBlank { "<empty>" } ?: "<empty>",
					before.pendingUpdateCount(),
				)
			}

			runCatching {
				log.timed("deleteWebhook") {
					val response = bot.execute(DeleteWebhook())
					if (response.isOk) {
						log.info("Telegram deleteWebhook ok")
					} else {
						log.warn(
							"Telegram deleteWebhook api error: {} {}",
							response.errorCode(),
							response.description(),
						)
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
					after.url()?.ifBlank { "<empty>" } ?: "<empty>",
					after.pendingUpdateCount(),
				)
			}
		}

	private fun getWebhookInfo() =
		log.timed("getWebhookInfo") {
			val response = bot.execute(GetWebhookInfo())
			if (!response.isOk) {
				log.warn(
					"Telegram getWebhookInfo api error: {} {}",
					response.errorCode(),
					response.description(),
				)
				return@timed null
			}
			response.webhookInfo()
		}

	private fun mapChatAction(action: String): ChatAction =
		when (action.lowercase()) {
			"typing" -> ChatAction.typing
			"upload_photo" -> ChatAction.upload_photo
			"upload_document" -> ChatAction.upload_document
			"find_location" -> ChatAction.find_location
			else -> ChatAction.typing
		}
}
