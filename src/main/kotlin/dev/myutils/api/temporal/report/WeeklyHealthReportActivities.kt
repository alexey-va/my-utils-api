package dev.myutils.api.temporal.report

import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod

@ActivityInterface
interface WeeklyHealthReportActivities {
	@ActivityMethod
	fun generateAndSend(input: WeeklyHealthReportActivityInput)
}
