package dev.myutils.api.temporal.notification

import dev.myutils.api.temporal.TemporalConstants
import dev.myutils.api.temporal.telegram.TelegramActivities
import io.temporal.activity.ActivityOptions
import io.temporal.spring.boot.WorkflowImpl
import io.temporal.workflow.Workflow
import java.time.Duration

@WorkflowImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
open class TelegramNotificationWorkflowImpl : TelegramNotificationWorkflow {
	private val telegram: TelegramActivities =
		Workflow.newActivityStub(
			TelegramActivities::class.java,
			ActivityOptions
				.newBuilder()
				.setStartToCloseTimeout(Duration.ofMinutes(2))
				.build(),
		)

	override fun deliver(input: NotificationWorkflowInput) {
		val delayMs = input.deliverAtEpochMillis - Workflow.currentTimeMillis()
		if (delayMs > 0) {
			Workflow.sleep(Duration.ofMillis(delayMs))
		}
		telegram.sendMessage(input.chatId, input.message)
	}
}
