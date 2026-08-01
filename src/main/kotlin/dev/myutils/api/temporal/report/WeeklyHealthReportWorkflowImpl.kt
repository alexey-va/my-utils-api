package dev.myutils.api.temporal.report

import dev.myutils.api.temporal.TemporalConstants
import io.temporal.activity.ActivityOptions
import io.temporal.common.RetryOptions
import io.temporal.spring.boot.WorkflowImpl
import io.temporal.workflow.Workflow
import java.time.DayOfWeek
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.temporal.TemporalAdjusters

@WorkflowImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
open class WeeklyHealthReportWorkflowImpl : WeeklyHealthReportWorkflow {
	private val activities: WeeklyHealthReportActivities =
		Workflow.newActivityStub(
			WeeklyHealthReportActivities::class.java,
			ActivityOptions
				.newBuilder()
				.setStartToCloseTimeout(Duration.ofMinutes(5))
				.setRetryOptions(
					RetryOptions
						.newBuilder()
						.setMaximumAttempts(3)
						.build(),
				).build(),
		)

	override fun run(input: WeeklyHealthReportInput) {
		while (true) {
			Workflow.sleep(durationUntilNextSaturdayNoon(input.zoneId, Workflow.currentTimeMillis()))
			val reportDate =
				Instant
					.ofEpochMilli(Workflow.currentTimeMillis())
					.atZone(ZoneId.of(input.zoneId))
					.toLocalDate()
			activities.generateAndSend(
				WeeklyHealthReportActivityInput(
					chatId = input.chatId,
					reportDate = reportDate.toString(),
					lookbackDays = input.lookbackDays.coerceIn(7, 366),
				),
			)
		}
	}
}

internal fun durationUntilNextSaturdayNoon(
	zoneId: String,
	nowMillis: Long,
): Duration {
	val now = Instant.ofEpochMilli(nowMillis).atZone(ZoneId.of(zoneId))
	var target =
		now
			.with(TemporalAdjusters.nextOrSame(DayOfWeek.SATURDAY))
			.withHour(12)
			.withMinute(0)
			.withSecond(0)
			.withNano(0)
	if (!target.isAfter(now)) {
		target = target.plusWeeks(1)
	}
	return Duration.between(now, target)
}

internal fun nextSaturdayNoon(
	zoneId: String,
	nowMillis: Long,
): ZonedDateTime =
	Instant.ofEpochMilli(nowMillis).atZone(ZoneId.of(zoneId)).plus(
		durationUntilNextSaturdayNoon(zoneId, nowMillis),
	)
