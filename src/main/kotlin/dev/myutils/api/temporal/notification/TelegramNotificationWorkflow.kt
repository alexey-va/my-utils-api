package dev.myutils.api.temporal.notification

import io.temporal.workflow.WorkflowInterface
import io.temporal.workflow.WorkflowMethod

@WorkflowInterface
interface TelegramNotificationWorkflow {
	@WorkflowMethod
	fun deliver(input: NotificationWorkflowInput)
}
