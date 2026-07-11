package dev.myutils.api.telegram

import com.pengrad.telegrambot.TelegramBot
import com.pengrad.telegrambot.model.request.ChatAction
import com.pengrad.telegrambot.model.request.Keyboard
import com.pengrad.telegrambot.model.request.ParseMode
import com.pengrad.telegrambot.request.AnswerCallbackQuery
import com.pengrad.telegrambot.request.DeleteMessage
import com.pengrad.telegrambot.request.EditMessageText
import com.pengrad.telegrambot.request.SendChatAction
import com.pengrad.telegrambot.request.SendMessage
import com.pengrad.telegrambot.request.SendPhoto
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
	): Int?

	fun editHtmlMessage(
		chatId: Long,
		messageId: Int,
		text: String,
	)

	fun deleteMessage(
		chatId: Long,
		messageId: Int,
	)

	fun sendTyping(chatId: Long)

	fun sendPhoto(
		chatId: Long,
		png: ByteArray,
		caption: String? = null,
	)

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
	): Int? {
		val request =
			SendMessage(chatId, text.take(TelegramLimits.MESSAGE_MAX_LENGTH))
				.parseMode(ParseMode.HTML)
		if (replyMarkup != null) {
			request.replyMarkup(replyMarkup)
		}
		val response = bot.execute(request)
		if (response.isOk) {
			log.info("Telegram send ok chatId={} text={}", chatId, LogPreview.of(text))
			return response.message()?.messageId()
		}
		log.warnTelegramFailed("send", response, " chatId=$chatId")
		return null
	}

	override fun editHtmlMessage(
		chatId: Long,
		messageId: Int,
		text: String,
	) {
		val response =
			bot.execute(
				EditMessageText(chatId, messageId, text.take(TelegramLimits.MESSAGE_MAX_LENGTH))
					.parseMode(ParseMode.HTML),
			)
		if (!response.isOk) {
			log.warnTelegramFailed("edit", response, " chatId=$chatId messageId=$messageId")
		}
	}

	override fun deleteMessage(
		chatId: Long,
		messageId: Int,
	) {
		val response = bot.execute(DeleteMessage(chatId, messageId))
		if (!response.isOk) {
			log.warnTelegramFailed("delete", response, " chatId=$chatId messageId=$messageId")
		}
	}

	override fun sendTyping(chatId: Long) {
		runCatching { bot.execute(SendChatAction(chatId, ChatAction.typing)) }
			.onFailure { ex -> log.debug("Telegram typing failed chatId={}", chatId, ex) }
	}

	override fun sendPhoto(
		chatId: Long,
		png: ByteArray,
		caption: String?,
	) {
		val request = SendPhoto(chatId, png)
		if (!caption.isNullOrBlank()) {
			request.caption(caption.take(TelegramLimits.MESSAGE_MAX_LENGTH)).parseMode(ParseMode.HTML)
		}
		val response = bot.execute(request)
		if (response.isOk) {
			log.info("Telegram photo ok chatId={} bytes={}", chatId, png.size)
		} else {
			log.warnTelegramFailed("sendPhoto", response, " chatId=$chatId")
		}
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
