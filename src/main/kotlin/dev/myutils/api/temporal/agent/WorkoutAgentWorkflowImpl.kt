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

	private val toolActivities: WorkoutToolActivities =
		Workflow.newActivityStub(
			WorkoutToolActivities::class.java,
			ActivityOptions
				.newBuilder()
				.setStartToCloseTimeout(Duration.ofMinutes(2))
				.setRetryOptions(
					RetryOptions
						.newBuilder()
						.setMaximumAttempts(3)
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
		val startedAt = Workflow.currentTimeMillis()
		var llmSteps = 0

		val prelude = agentActivities.resolvePrelude(input)
		if (prelude.kind == AgentPreludeResult.Kind.REPLY) {
			recordTurnMetrics(startedAt, llmSteps, preludeOutcome(input, prelude.message))
			telegramActivities.sendMessage(input.chatId, prelude.message.orEmpty())
			return
		}

		var userMessage: String? = input.text
		val maxSteps = input.maxToolIterations.coerceAtLeast(1)
		repeat(maxSteps) {
			llmSteps++
			val step = agentActivities.llmStep(AgentLlmStepInput(input.chatId, userMessage))
			userMessage = null

			if (!step.hasToolCalls) {
				val reply = step.reply.trim().ifEmpty { "Готово." }
				recordTurnMetrics(startedAt, llmSteps, "reply")
				telegramActivities.sendMessage(input.chatId, reply)
				return
			}

			val toolResults =
				step.toolCalls.map { tool ->
					ToolCallResultDto(
						toolCallId = tool.id,
						toolName = tool.name,
						result =
							toolActivities.executeTool(
								ToolCallInput(
									chatId = input.chatId,
									toolName = tool.name,
									argumentsJson = tool.argumentsJson,
								),
							),
					)
				}
			agentActivities.recordToolResults(
				RecordToolResultsInput(
					chatId = input.chatId,
					results = toolResults,
				),
			)
		}

		recordTurnMetrics(startedAt, llmSteps, "tool_limit")
		telegramActivities.sendMessage(input.chatId, "Слишком много шагов с инструментами, попробуй короче.")
	}

	private fun recordTurnMetrics(
		startedAt: Long,
		llmSteps: Int,
		outcome: String,
	) {
		agentActivities.recordTurnMetrics(
			AgentTurnMetricsInput(
				outcome = outcome,
				durationMs = (Workflow.currentTimeMillis() - startedAt).coerceAtLeast(0),
				llmSteps = llmSteps,
			),
		)
	}

	private fun preludeOutcome(
		input: AgentTurnInput,
		message: String?,
	): String =
		when {
			input.text == "/start" -> "start_command"
			message?.contains("нет доступа") == true -> "rejected"
			else -> "prelude_reply"
		}
}
