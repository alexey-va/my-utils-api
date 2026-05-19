package dev.myutils.api.temporal.notification

/** Доставка сообщения в Telegram не раньше [deliverAtEpochMillis] (workflow time). */
data class NotificationWorkflowInput(
	val chatId: Long,
	val message: String,
	val deliverAtEpochMillis: Long,
)
