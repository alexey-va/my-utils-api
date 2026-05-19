package dev.myutils.api.temporal.reminder

/** Аргументы долгоживущего workflow ежедневного напоминания. */
data class ReminderWorkflowInput(
	val chatId: Long,
	val zoneId: String = "Europe/Moscow",
	val hour: Int = 20,
	val minute: Int = 0,
)
