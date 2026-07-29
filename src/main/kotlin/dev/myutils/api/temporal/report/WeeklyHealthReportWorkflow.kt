package dev.myutils.api.temporal.report

import io.temporal.workflow.WorkflowInterface
import io.temporal.workflow.WorkflowMethod

@WorkflowInterface
interface WeeklyHealthReportWorkflow {
	@WorkflowMethod
	fun run(input: WeeklyHealthReportInput)
}
