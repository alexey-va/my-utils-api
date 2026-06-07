package dev.myutils.api.temporal.agent

import dev.myutils.api.agent.AgentToolCatalog
import dev.myutils.api.infra.util.LogPreview
import dev.myutils.api.temporal.TemporalConstants
import dev.myutils.api.temporal.logging.TemporalWorkflowLog
import dev.myutils.api.temporal.telegram.TelegramActivities
import io.temporal.activity.ActivityOptions
import io.temporal.common.RetryOptions
import io.temporal.spring.boot.WorkflowImpl
import io.temporal.workflow.Async
import io.temporal.workflow.Promise
import io.temporal.workflow.Workflow
import java.time.Duration

@WorkflowImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
open class WorkoutAgentWorkflowImpl : WorkoutAgentWorkflow {
	private val log = TemporalWorkflowLog.of<WorkoutAgentWorkflowImpl>()
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

		log.info(
			"Agent turn started",
			mapOf(
				"chatId" to input.chatId,
				"userId" to input.userId,
				"text" to LogPreview.of(input.text),
				"maxToolIterations" to input.maxToolIterations,
			),
		)

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
			log.info(
				"Agent LLM step",
				mapOf(
					"step" to llmSteps,
					"chatId" to input.chatId,
					"userMessage" to (userMessage?.let { LogPreview.of(it) } ?: "(none)"),
				),
			)
			val step = agentActivities.llmStep(AgentLlmStepInput(input.chatId, userMessage))
			userMessage = null

			if (!step.hasToolCalls) {
				log.info(
					"Agent LLM step finished",
					mapOf(
						"step" to llmSteps,
						"chatId" to input.chatId,
						"outcome" to "reply",
						"reply" to LogPreview.of(step.reply),
					),
				)
				val reply = step.reply.trim().ifEmpty { "Готово." }
				recordTurnMetrics(startedAt, llmSteps, "reply")
				telegramActivities.sendMessage(input.chatId, reply)
				return
			}

			log.info(
				"Agent LLM step tool calls",
				mapOf(
					"step" to llmSteps,
					"chatId" to input.chatId,
					"parallel" to (step.toolCalls.size > 1),
					"toolCallCount" to step.toolCalls.size,
					"toolCalls" to
						step.toolCalls.joinToString {
							"${it.name}(${it.id} args=${LogPreview.of(it.argumentsJson, 120)})"
						},
				),
			)
			val toolResults = executeToolCallsParallel(input.chatId, step.toolCalls)
			log.info(
				"Agent LLM step tool results",
				mapOf(
					"step" to llmSteps,
					"chatId" to input.chatId,
					"toolResults" to
						toolResults.joinToString {
							"${it.toolName}(${it.toolCallId}): ${LogPreview.of(it.result, 200)}"
						},
				),
			)
			agentActivities.recordToolResults(
				RecordToolResultsInput(
					chatId = input.chatId,
					results = toolResults,
				),
			)
			if (isImmediateReturnStep(step.toolCalls)) {
				log.info(
					"Agent immediate tool step finished",
					mapOf(
						"step" to llmSteps,
						"chatId" to input.chatId,
						"tools" to step.toolCalls.joinToString { it.name },
					),
				)
				recordTurnMetrics(startedAt, llmSteps, "immediate_tool")
				return
			}
		}

		log.info(
			"Agent turn tool limit",
			mapOf(
				"chatId" to input.chatId,
				"llmSteps" to llmSteps,
				"maxToolIterations" to maxSteps,
			),
		)
		recordTurnMetrics(startedAt, llmSteps, "tool_limit")
		telegramActivities.sendMessage(input.chatId, "Слишком много шагов с инструментами, попробуй короче.")
	}

	private fun executeToolCallsParallel(
		chatId: Long,
		toolCalls: List<ToolCallDto>,
	): List<ToolCallResultDto> {
		if (toolCalls.isEmpty()) {
			return emptyList()
		}
		if (toolCalls.size == 1) {
			val tool = toolCalls.first()
			return listOf(executeToolCall(chatId, tool))
		}

		val promises = ArrayList<Promise<String>>(toolCalls.size)
		for (tool in toolCalls) {
			promises.add(
				Async.function(
					toolActivities::executeTool,
					ToolCallInput(
						chatId = chatId,
						toolName = tool.name,
						argumentsJson = tool.argumentsJson,
					),
				),
			)
		}
		Promise.allOf(promises).get()
		return toolCalls.indices.map { index ->
			val tool = toolCalls[index]
			ToolCallResultDto(
				toolCallId = tool.id,
				toolName = tool.name,
				result = promises[index].get(),
			)
		}
	}

	private fun isImmediateReturnStep(toolCalls: List<ToolCallDto>): Boolean =
		toolCalls.isNotEmpty() && toolCalls.all { AgentToolCatalog.isImmediateReturn(it.name) }

	private fun executeToolCall(
		chatId: Long,
		tool: ToolCallDto,
	): ToolCallResultDto =
		ToolCallResultDto(
			toolCallId = tool.id,
			toolName = tool.name,
			result =
				toolActivities.executeTool(
					ToolCallInput(
						chatId = chatId,
						toolName = tool.name,
						argumentsJson = tool.argumentsJson,
					),
				),
		)

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
