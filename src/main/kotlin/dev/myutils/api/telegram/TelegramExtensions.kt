package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.model.request.ChatAction
import com.pengrad.telegrambot.model.request.Keyboard
import com.pengrad.telegrambot.model.request.ParseMode
import com.pengrad.telegrambot.request.AnswerCallbackQuery
import com.pengrad.telegrambot.request.SendChatAction
import com.pengrad.telegrambot.request.SendMessage
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory

private val log = LoggerFactory.getLogger("dev.myutils.api.telegram")

fun TelegramBot.sendHtmlMessage(
	chatId: Long,
	text: String,
	replyMarkup: Keyboard? = null,
) {
	val request =
		SendMessage(chatId, text.take(4096))
			.parseMode(ParseMode.HTML)
	if (replyMarkup != null) {
		request.replyMarkup(replyMarkup)
	}
	val response = execute(request)
	if (response.isOk) {
		log.info("Telegram send ok chatId={} text={}", chatId, LogPreview.of(text))
	} else {
		log.warn(
			"Telegram send failed chatId={}: {} {}",
			chatId,
			response.errorCode(),
			response.description(),
		)
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
		request.text(text.take(200))
	}
	val response = execute(request)
	if (!response.isOk) {
		log.warn(
			"Telegram answerCallback failed: {} {}",
			response.errorCode(),
			response.description(),
		)
	}
}
