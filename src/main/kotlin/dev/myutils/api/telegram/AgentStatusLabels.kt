package dev.myutils.api.telegram

import dev.myutils.api.agent.AgentToolCatalog

/** Человекочитаемые подписи для статуса агента в Telegram (как progress в Cursor). */
object AgentStatusLabels {
	fun thinking(step: Int = 1): String =
		if (step <= 1) {
			"Думаю…"
		} else {
			"Думаю (шаг $step)…"
		}

	const val COMPOSING_REPLY: String = "Формирую ответ…"

	fun toolRunning(rawName: String): String = AgentToolCatalog.statusLabel(rawName)

	fun toolsRunning(rawNames: List<String>): String {
		if (rawNames.isEmpty()) {
			return "Обрабатываю…"
		}
		val distinct =
			rawNames
				.map { AgentToolCatalog.normalizeName(it) }
				.distinct()
		return when (distinct.size) {
			1 -> toolRunning(rawNames.first())
			else -> "Выполняю ${distinct.size} действия…"
		}
	}
}
