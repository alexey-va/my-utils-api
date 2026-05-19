package dev.myutils.api.temporal.reminder

import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod

@ActivityInterface
interface WorkoutReminderActivities {
	@ActivityMethod
	fun hasWorkoutLoggedToday(zoneId: String): Boolean

	@ActivityMethod
	fun sendEveningReminder(chatId: Long)
}
