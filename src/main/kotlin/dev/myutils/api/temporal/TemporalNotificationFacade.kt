package dev.myutils.api.temporal

import dev.myutils.api.config.MyUtilsProperties
import org.springframework.beans.factory.ObjectProvider
import org.springframework.stereotype.Component
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException

@Component
class TemporalNotificationFacade(
	private val properties: MyUtilsProperties,
	private val temporalWorkflowService: ObjectProvider<TemporalWorkflowService>,
) {
	private val zoneId: ZoneId
		get() = ZoneId.of(properties.temporal.zoneId)

	fun sendNow(
		chatId: Long,
		message: String,
	): String {
		val temporal = temporalWorkflowService.getIfAvailable()
			?: return "Temporal выключен — уведомление не отправлено."
		if (message.isBlank()) {
			return "Текст уведомления пустой."
		}
		val workflowId = temporal.sendNotificationNow(chatId, message)
		return "Уведомление отправляется сейчас (workflow $workflowId)."
	}

	fun schedule(
		chatId: Long,
		message: String,
		deliverAtRaw: String,
	): String {
		val temporal = temporalWorkflowService.getIfAvailable()
			?: return "Temporal выключен — напоминание не запланировано."
		if (message.isBlank()) {
			return "Текст уведомления пустой."
		}
		val deliverAt =
			parseDeliverAt(deliverAtRaw)
				?: return "Неверный deliver_at: укажи ISO дату-время, например 2026-05-20T20:00:00+03:00"
		if (!deliverAt.isAfter(ZonedDateTime.now(zoneId))) {
			return "deliver_at должен быть в будущем (часовой пояс ${properties.temporal.zoneId})."
		}
		val workflowId = temporal.scheduleNotification(chatId, message, deliverAt)
		val formatted = DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm").format(deliverAt)
		return "Напоминание запланировано на $formatted (workflow $workflowId)."
	}

	fun cancel(workflowId: String): String {
		val temporal = temporalWorkflowService.getIfAvailable()
			?: return "Temporal выключен."
		return if (temporal.cancelNotification(workflowId.trim())) {
			"Напоминание отменено ($workflowId)."
		} else {
			"Workflow не найден: $workflowId"
		}
	}

	private fun parseDeliverAt(raw: String): ZonedDateTime? {
		val trimmed = raw.trim()
		return try {
			ZonedDateTime.parse(trimmed)
		} catch (_: DateTimeParseException) {
			try {
				LocalDateTime.parse(trimmed).atZone(zoneId)
			} catch (_: DateTimeParseException) {
				null
			}
		}
	}
}
