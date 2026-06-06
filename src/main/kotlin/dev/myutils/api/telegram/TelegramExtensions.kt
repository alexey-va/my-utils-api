package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.model.request.ChatAction
import com.pengrad.telegrambot.model.request.Keyboard
import com.pengrad.telegrambot.model.request.ParseMode
import com.pengrad.telegrambot.request.AnswerCallbackQuery
import com.pengrad.telegrambot.request.SendChatAction
import com.pengrad.telegrambot.request.SendMessage
import com.pengrad.telegrambot.response.BaseResponse
import dev.myutils.api.util.LogPreview
import org.slf4j.Logger
import org.slf4j.LoggerFactory

private val log = LoggerFactory.getLogger("dev.myutils.api.telegram")

fun TelegramBot.sendHtmlMessage(
	chatId: Long,
	text: String,
	replyMarkup: Keyboard? = null,
) {
	val request =
		SendMessage(chatId, text.take(TelegramLimits.MESSAGE_MAX_LENGTH))
			.parseMode(ParseMode.HTML)
	if (replyMarkup != null) {
		request.replyMarkup(replyMarkup)
	}
	val response = execute(request)
	if (response.isOk) {
		log.info("Telegram send ok chatId={} text={}", chatId, LogPreview.of(text))
	} else {
		log.warnTelegramFailed("send", response, " chatId=$chatId")
	}
}

fun TelegramBot.sendTyping(chatId: Long) {
	runCatching { execute(SendChatAction(chatId, ChatAction.typing)) }
		.onFailure { ex -> log.debug("Telegram typing failed chatId={}", chatId, ex) }
}

fun TelegramBot.answerCallback(
	callbackQueryId: String,
	text: String? = null,
) {
	val request = AnswerCallbackQuery(callbackQueryId)
	if (!text.isNullOrBlank()) {
		request.text(text.take(TelegramLimits.CALLBACK_ANSWER_MAX_LENGTH))
	}
	val response = execute(request)
	log.warnTelegramFailed("answerCallback", response)
}

private fun Logger.warnTelegramFailed(
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
