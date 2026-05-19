package dev.myutils.api.temporal.reminder

import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.telegram.TelegramClient
import dev.myutils.api.temporal.TemporalConstants
import io.temporal.spring.boot.ActivityImpl
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Component
import java.time.LocalDate
import java.time.ZoneId

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ActivityImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
class WorkoutReminderActivitiesImpl(
	private val workoutBotFacade: WorkoutBotFacade,
	private val telegramClient: ObjectProvider<TelegramClient>,
) : WorkoutReminderActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun hasWorkoutLoggedToday(zoneId: String): Boolean {
		val today = LocalDate.now(ZoneId.of(zoneId))
		val logged = workoutBotFacade.hasWorkoutEntriesOn(today)
		log.info("Temporal activity hasWorkoutLoggedToday zone={} date={} logged={}", zoneId, today, logged)
		return logged
	}

	override fun sendEveningReminder(chatId: Long) {
		val client =
			telegramClient.getIfAvailable()
				?: run {
					log.warn("Temporal evening reminder skipped: Telegram bot not configured")
					return
				}
		client.sendMessage(
			chatId,
			"Сегодня в дневнике пусто. Запиши тренировку или напиши «что на сегодня» — составлю план.",
		)
	}
}
