package dev.myutils.api.temporal.agent

import dev.myutils.api.temporal.TemporalConstants
import dev.myutils.api.temporal.telegram.TelegramActivities
import io.temporal.activity.ActivityOptions
import io.temporal.common.RetryOptions
import io.temporal.spring.boot.WorkflowImpl
import io.temporal.workflow.Workflow
import java.time.Duration

@WorkflowImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
open class WorkoutAgentWorkflowImpl : WorkoutAgentWorkflow {
	private val agentActivities: WorkoutAgentActivities =
		Workflow.newActivityStub(
			WorkoutAgentActivities::class.java,
			ActivityOptions
				.newBuilder()
				.setStartToCloseTimeout(Duration.ofMinutes(6))
				.setRetryOptions(
					RetryOptions
						.newBuilder()
						.setMaximumAttempts(2)
						.build(),
				).build(),
		)

	private val telegramActivities: TelegramActivities =
		Workflow.newActivityStub(
			TelegramActivities::class.java,
			ActivityOptions
				.newBuilder()
				.setStartToCloseTimeout(Duration.ofMinutes(2))
				.setRetryOptions(
					RetryOptions
						.newBuilder()
						.setMaximumAttempts(5)
						.build(),
				).build(),
		)

	override fun handleTurn(input: AgentTurnInput) {
		val reply = agentActivities.runAgent(input)
		telegramActivities.sendMessage(input.chatId, reply)
	}
}
