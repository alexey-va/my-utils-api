package dev.myutils.api.temporal.reminder

import io.temporal.workflow.WorkflowInterface
import io.temporal.workflow.WorkflowMethod

@WorkflowInterface
interface EveningWorkoutReminderWorkflow {
	@WorkflowMethod
	fun run(input: ReminderWorkflowInput)
}
