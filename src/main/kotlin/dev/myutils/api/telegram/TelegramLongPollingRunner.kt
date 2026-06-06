package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.UpdatesListener
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.telegram.TelegramApiSupport.describeError
import jakarta.annotation.PreDestroy
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.slf4j.LoggerFactory
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component

/** Receives bot updates via pengrad long-polling listener (messages + inline button callbacks). */
@Component
@ConditionalOnTelegramBot
class TelegramLongPollingRunner(
	private val properties: MyUtilsProperties,
	private val bot: TelegramBot,
	private val telegramClient: TelegramClient,
	private val inboundCoalescer: TelegramInboundCoalescer,
	private val telegramScope: TelegramCoroutineScope,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@EventListener(ApplicationReadyEvent::class)
	fun start() {
		val allowed = properties.telegram.allowedUserIdSet()
		log.info(
			"Telegram bot starting long polling allowedUsers={}",
			allowed.ifEmpty { "any" },
		)
		runBlocking {
			telegramClient.ensureLongPollingMode()
		}
		bot.setUpdatesListener(
			{ updates ->
				for (update in updates) {
					dispatchUpdate(update)
				}
				UpdatesListener.CONFIRMED_UPDATES_ALL
			},
			{ error ->
				if (error.response() != null) {
					val response = error.response()
					log.warn(
						"Telegram polling api error: {} {}",
						response.errorCode(),
						response.description(),
					)
				} else {
					log.warn("Telegram polling error: {}", describeError(error), error)
				}
			},
		)
		log.info("Telegram long polling active")
	}

	@PreDestroy
	fun stop() {
		bot.removeGetUpdatesListener()
		log.info("Telegram long polling stopped")
	}

	private fun dispatchUpdate(update: com.pengrad.telegrambot.model.Update) {
		val callbackQuery = update.callbackQuery()
		if (callbackQuery != null) {
			val userId = callbackQuery.from()?.id() ?: return
			val chatId = callbackQuery.message()?.chat()?.id() ?: return
			val data = callbackQuery.data()?.trim().orEmpty()
			if (data.isEmpty()) {
				return
			}
			telegramScope.launch {
				telegramClient.answerCallbackQuery(callbackQuery.id())
				log.info(
					"Telegram callback chatId={} userId={} data={}",
					chatId,
					userId,
					data,
				)
				inboundCoalescer.enqueue(chatId, userId, data)
			}
			return
		}

		val message = update.message() ?: update.editedMessage() ?: return
		val userId = message.from()?.id() ?: return
		val text = message.text()?.trim().orEmpty()
		if (text.isEmpty()) {
			return
		}
		telegramScope.launch {
			inboundCoalescer.enqueue(message.chat().id(), userId, text)
		}
	}
}
