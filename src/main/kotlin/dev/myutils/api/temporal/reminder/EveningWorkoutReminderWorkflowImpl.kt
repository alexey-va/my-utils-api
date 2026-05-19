package dev.myutils.api.temporal.reminder

import dev.myutils.api.temporal.TemporalConstants
import io.temporal.activity.ActivityOptions
import io.temporal.spring.boot.WorkflowImpl
import io.temporal.workflow.Workflow
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

@WorkflowImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
open class EveningWorkoutReminderWorkflowImpl : EveningWorkoutReminderWorkflow {
	private val activities: WorkoutReminderActivities =
		Workflow.newActivityStub(
			WorkoutReminderActivities::class.java,
			ActivityOptions
				.newBuilder()
				.setStartToCloseTimeout(Duration.ofMinutes(2))
				.build(),
		)

	override fun run(input: ReminderWorkflowInput) {
		while (true) {
			Workflow.sleep(sleepUntilNextReminder(input, Workflow.currentTimeMillis()))
			if (!activities.hasWorkoutLoggedToday(input.zoneId)) {
				activities.sendEveningReminder(input.chatId)
			}
		}
	}

	private fun sleepUntilNextReminder(
		input: ReminderWorkflowInput,
		nowMillis: Long,
	): Duration {
		val zone = ZoneId.of(input.zoneId)
		val now = Instant.ofEpochMilli(nowMillis).atZone(zone)
		var target =
			now
				.withHour(input.hour)
				.withMinute(input.minute)
				.withSecond(0)
				.withNano(0)
		if (!target.isAfter(now)) {
			target = target.plusDays(1)
		}
		return Duration.between(now, target)
	}
}
