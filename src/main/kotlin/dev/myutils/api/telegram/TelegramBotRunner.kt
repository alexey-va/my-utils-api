package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.UpdatesListener
import com.pengrad.telegrambot.model.CallbackQuery
import com.pengrad.telegrambot.model.Message
import com.pengrad.telegrambot.model.Update
import com.pengrad.telegrambot.request.DeleteWebhook
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import jakarta.annotation.PreDestroy
import org.slf4j.LoggerFactory
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.stereotype.Component

/** Starts pengrad long-polling and routes updates to the agent coalescer. */
@Component
@ConditionalOnTelegramBot
@ConditionalOnProperty(prefix = "myutils.telegram", name = ["polling-enabled"], havingValue = "true", matchIfMissing = true)
class TelegramBotRunner(
	private val properties: MyUtilsProperties,
	private val bot: TelegramBot,
	private val messenger: TelegramMessenger,
	private val inboundCoalescer: TelegramInboundCoalescer,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@EventListener(ApplicationReadyEvent::class)
	fun start() {
		val allowed = properties.telegram.allowedUserIdSet()
		log.info(
			"Telegram bot starting allowedUsers={}",
			allowed.ifEmpty { "any" },
		)

		val webhookResponse = bot.execute(DeleteWebhook().dropPendingUpdates(true))
		if (webhookResponse.isOk) {
			log.info("Telegram deleteWebhook ok (pending updates dropped)")
		} else {
			log.warn(
				"Telegram deleteWebhook failed: {} {}",
				webhookResponse.errorCode(),
				webhookResponse.description(),
			)
		}

		bot.setUpdatesListener(
			{ updates ->
				for (update in updates) {
					routeUpdate(update)
				}
				UpdatesListener.CONFIRMED_UPDATES_ALL
			},
			{ error ->
				val response = error.response()
				if (response != null) {
					log.warn(
						"Telegram polling api error: {} {}",
						response.errorCode(),
						response.description(),
					)
				} else {
					log.warn("Telegram polling error: {}", error.message, error)
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

	private fun routeUpdate(update: Update) {
		update.callbackQuery()?.let { callback ->
			routeCallback(callback)
			return
		}
		val message = update.message() ?: update.editedMessage() ?: return
		routeMessage(message)
	}

	private fun routeCallback(callback: CallbackQuery) {
		val userId = callback.from()?.id() ?: return
		val chatId = callback.message()?.chat()?.id() ?: return
		val data = callback.data()?.trim().orEmpty()
		if (data.isEmpty()) {
			return
		}
		messenger.answerCallback(callback.id())
		log.info("Telegram callback chatId={} userId={} data={}", chatId, userId, data)
		inboundCoalescer.enqueue(chatId, userId, data)
	}

	private fun routeMessage(message: Message) {
		val userId = message.from()?.id() ?: return
		val text = message.text()?.trim().orEmpty()
		if (text.isEmpty()) {
			return
		}
		inboundCoalescer.enqueue(message.chat().id(), userId, text)
	}
}
