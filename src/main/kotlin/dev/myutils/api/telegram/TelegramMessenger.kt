package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.model.request.ChatAction
import com.pengrad.telegrambot.model.request.Keyboard
import com.pengrad.telegrambot.model.request.ParseMode
import com.pengrad.telegrambot.request.AnswerCallbackQuery
import com.pengrad.telegrambot.request.SendChatAction
import com.pengrad.telegrambot.request.SendMessage
import com.pengrad.telegrambot.response.BaseResponse
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

/** Outbound Telegram API — production uses pengrad, tests override via @Primary fakes. */
interface TelegramMessenger {
	fun sendHtmlMessage(
		chatId: Long,
		text: String,
		replyMarkup: Keyboard? = null,
	)

	fun sendTyping(chatId: Long)

	fun answerCallback(
		callbackQueryId: String,
		text: String? = null,
	)
}

@Component
@ConditionalOnTelegramBot
class PengradTelegramMessenger(
	private val bot: TelegramBot,
) : TelegramMessenger {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun sendHtmlMessage(
		chatId: Long,
		text: String,
		replyMarkup: Keyboard?,
	) {
		val request =
			SendMessage(chatId, text.take(TelegramLimits.MESSAGE_MAX_LENGTH))
				.parseMode(ParseMode.HTML)
		if (replyMarkup != null) {
			request.replyMarkup(replyMarkup)
		}
		val response = bot.execute(request)
		if (response.isOk) {
			log.info("Telegram send ok chatId={} text={}", chatId, LogPreview.of(text))
		} else {
			log.warnTelegramFailed("send", response, " chatId=$chatId")
		}
	}

	override fun sendTyping(chatId: Long) {
		runCatching { bot.execute(SendChatAction(chatId, ChatAction.typing)) }
			.onFailure { ex -> log.debug("Telegram typing failed chatId={}", chatId, ex) }
	}

	override fun answerCallback(
		callbackQueryId: String,
		text: String?,
	) {
		val request = AnswerCallbackQuery(callbackQueryId)
		if (!text.isNullOrBlank()) {
			request.text(text.take(TelegramLimits.CALLBACK_ANSWER_MAX_LENGTH))
		}
		val response = bot.execute(request)
		log.warnTelegramFailed("answerCallback", response)
	}

	private fun org.slf4j.Logger.warnTelegramFailed(
		operation: String,
		response: BaseResponse,
		context: String = "",
	) {
		if (!response.isOk) {
			warn(
				"Telegram {} failed{}: {} {}",
				operation,
				context,
				response.errorCode(),
				response.description(),
			)
		}
	}
}
