package dev.myutils.api.telegram

import com.pengrad.telegrambot.model.request.InlineKeyboardButton
import com.pengrad.telegrambot.model.request.InlineKeyboardMarkup

/** Helpers for inline keyboards (callback buttons). */
object TelegramKeyboards {
	fun inlineRow(vararg buttons: Pair<String, String>): InlineKeyboardMarkup =
		InlineKeyboardMarkup(
			buttons.map { (label, callbackData) ->
				InlineKeyboardButton(label).callbackData(callbackData)
			}.toTypedArray(),
		)

	fun inlineGrid(rows: List<List<Pair<String, String>>>): InlineKeyboardMarkup {
		val keyboardRows =
			rows
				.map { row ->
					row
						.map { (label, callbackData) ->
							InlineKeyboardButton(label).callbackData(callbackData)
						}.toTypedArray()
				}.toTypedArray()
		return InlineKeyboardMarkup(*keyboardRows)
	}
}
