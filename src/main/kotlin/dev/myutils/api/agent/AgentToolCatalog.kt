package dev.myutils.api.agent

/** Метаданные инструментов агента (в т.ч. для Temporal workflow). */
object AgentToolCatalog {
	private val immediateReturnTools =
		setOf(
			"send_rich_message",
			"send_progress_chart",
		)

	/** Человекочитаемый статус в Telegram — у каждого тула своя подпись. */
	private val statusLabels: Map<String, String> =
		mapOf(
			"list_exercises" to "Загружаю список упражнений…",
			"create_exercise" to "Создаю упражнение…",
			"rename_exercise" to "Переименовываю упражнение…",
			"log_workout" to "Записываю в дневник…",
			"delete_workout" to "Удаляю запись…",
			"get_exercise_progresses" to "Получаю прогресс…",
			"get_progress" to "Получаю прогресс…",
			"get_day_summaries" to "Получаю статистику по дням…",
			"get_days" to "Получаю статистику по дням…",
			"remember_fact" to "Запоминаю факт…",
			"forget_fact" to "Удаляю из памяти…",
			"manage_user_fact" to "Обновляю память…",
			"send_rich_message" to "Отправляю сообщение с кнопками…",
			"send_progress_chart" to "Строю график прогресса…",
			"send_notification" to "Отправляю уведомление…",
			"schedule_notification" to "Планирую напоминание…",
			"cancel_notification" to "Отменяю напоминание…",
		)

	fun isImmediateReturn(toolName: String): Boolean = normalizeName(toolName) in immediateReturnTools

	fun statusLabel(toolName: String): String {
		val key = normalizeName(toolName)
		return statusLabels[key] ?: error("Нет статус-подписи для tool: $key")
	}

	fun registeredToolNames(): Set<String> = statusLabels.keys

	fun normalizeName(toolName: String): String = camelToSnake(toolName)

	private fun camelToSnake(value: String): String =
		value
			.replace(Regex("([a-z0-9])([A-Z])")) { "${it.groupValues[1]}_${it.groupValues[2]}" }
			.lowercase()
}
