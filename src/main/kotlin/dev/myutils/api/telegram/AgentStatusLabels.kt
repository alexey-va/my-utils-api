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

	fun toolRunning(rawName: String): String =
		when (AgentToolCatalog.normalizeName(rawName)) {
			"log_workout" -> "Записываю в дневник…"
			"delete_workout" -> "Удаляю запись…"
			"get_day_summaries", "get_days" -> "Получаю статистику по дням…"
			"get_exercise_progresses", "get_progress" -> "Получаю прогресс…"
			"list_exercises" -> "Загружаю список упражнений…"
			"create_exercise" -> "Создаю упражнение…"
			"rename_exercise" -> "Переименовываю упражнение…"
			"remember_fact", "manage_user_fact" -> "Запоминаю факт…"
			"forget_fact" -> "Удаляю из памяти…"
			"send_rich_message" -> "Отправляю сообщение с кнопками…"
			"send_notification" -> "Отправляю уведомление…"
			"schedule_notification" -> "Планирую напоминание…"
			"cancel_notification" -> "Отменяю напоминание…"
			else -> "Выполняю ${AgentToolCatalog.normalizeName(rawName)}…"
		}

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
