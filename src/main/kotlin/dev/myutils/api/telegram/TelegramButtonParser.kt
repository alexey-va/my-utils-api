package dev.myutils.api.telegram

import com.pengrad.telegrambot.model.request.InlineKeyboardMarkup

/**
 * Разбор кнопок для LLM-tool.
 *
 * Формат [buttons]:
 * - ряды через `;`
 * - кнопки в ряду через `,`
 * - каждая кнопка: `подпись:callback`
 *
 * Пример: `Сегодня:что на сегодня,Неделя:план;Отмена:отмена`
 * callback уходит в агента как обычный текст (см. TelegramBotRunner).
 */
object TelegramButtonParser {
	fun parse(buttons: String?): InlineKeyboardMarkup? {
		val rows = parseRows(buttons) ?: return null
		return TelegramKeyboards.inlineGrid(rows)
	}

	fun parseRows(buttons: String?): List<List<Pair<String, String>>>? {
		val trimmed = buttons?.trim().orEmpty()
		if (trimmed.isEmpty()) {
			return null
		}
		val rows =
			trimmed.split(";").mapNotNull { rowRaw ->
				val row =
					rowRaw
						.split(",")
						.mapNotNull { buttonRaw -> parseButton(buttonRaw) }
				row.takeIf { it.isNotEmpty() }
			}
		if (rows.isEmpty()) {
			throw IllegalArgumentException(
				"Неверный формат buttons. Пример: Сегодня:что на сегодня,Неделя:план",
			)
		}
		return rows
	}

	private fun parseButton(raw: String): Pair<String, String>? {
		val part = raw.trim()
		if (part.isEmpty()) {
			return null
		}
		val colon = part.indexOf(':')
		if (colon <= 0 || colon == part.lastIndex) {
			throw IllegalArgumentException("Кнопка должна быть в формате подпись:callback, получено: $part")
		}
		val label = part.substring(0, colon).trim()
		val callback = part.substring(colon + 1).trim()
		if (label.isEmpty() || callback.isEmpty()) {
			throw IllegalArgumentException("Пустая подпись или callback в кнопке: $part")
		}
		if (callback.length > TelegramLimits.CALLBACK_DATA_MAX_LENGTH) {
			throw IllegalArgumentException(
				"callback слишком длинный (макс. ${TelegramLimits.CALLBACK_DATA_MAX_LENGTH}): $callback",
			)
		}
		return label to callback
	}
}
