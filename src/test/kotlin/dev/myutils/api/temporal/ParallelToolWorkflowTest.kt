package dev.myutils.api.temporal

import dev.myutils.api.temporal.agent.AgentLlmStepInput
import dev.myutils.api.temporal.agent.AgentLlmStepResult
import dev.myutils.api.temporal.agent.AgentPreludeResult
import dev.myutils.api.temporal.agent.AgentTurnInput
import dev.myutils.api.temporal.agent.AgentTurnMetricsInput
import dev.myutils.api.temporal.agent.RecordToolResultsInput
import dev.myutils.api.temporal.agent.ToolCallDto
import dev.myutils.api.temporal.agent.ToolCallInput
import dev.myutils.api.temporal.agent.WorkoutAgentActivities
import dev.myutils.api.temporal.agent.WorkoutAgentWorkflow
import dev.myutils.api.temporal.agent.WorkoutAgentWorkflowImpl
import dev.myutils.api.temporal.agent.WorkoutToolActivities
import dev.myutils.api.temporal.telegram.TelegramActivities
import io.temporal.client.WorkflowClient
import io.temporal.client.WorkflowOptions
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import java.time.Duration

class ParallelToolWorkflowTest {
	private lateinit var testEnv: io.temporal.testing.TestWorkflowEnvironment
	private val toolCalls = mutableListOf<ToolCallInput>()

	@BeforeEach
	fun setUp() {
		toolCalls.clear()
		testEnv = TemporalTestSupport.create()
		val worker = testEnv.newWorker(TemporalConstants.TASK_QUEUE)
		worker.registerWorkflowImplementationTypes(WorkoutAgentWorkflowImpl::class.java)
		var llmStepCount = 0
		worker.registerActivitiesImplementations(
			object : WorkoutAgentActivities {
				override fun resolvePrelude(input: AgentTurnInput): AgentPreludeResult =
					AgentPreludeResult(AgentPreludeResult.Kind.CONTINUE)

				override fun llmStep(input: AgentLlmStepInput): AgentLlmStepResult {
					llmStepCount++
					return if (llmStepCount == 1) {
						AgentLlmStepResult(
							toolCalls =
								listOf(
									ToolCallDto("tc-1", "getDaySummaries", """{"days":"2026-06-07"}"""),
									ToolCallDto("tc-2", "listExercises", "{}"),
								),
						)
					} else {
						AgentLlmStepResult(reply = "stub reply")
					}
				}

				override fun recordToolResults(input: RecordToolResultsInput) = Unit

				override fun recordTurnMetrics(input: AgentTurnMetricsInput) = Unit
			},
			object : WorkoutToolActivities {
				override fun executeTool(input: ToolCallInput): String {
					toolCalls.add(input)
					return "ok"
				}
			},
			object : TelegramActivities {
				override fun sendMessage(
					chatId: Long,
					text: String,
				) = Unit

				override fun updateAgentStatus(
					chatId: Long,
					text: String,
				) = Unit

				override fun completeAgentStatus(chatId: Long) = Unit

				override fun failAgentStatus(
					chatId: Long,
					text: String,
				) = Unit
			},
		)
		testEnv.start()
	}

	@AfterEach
	fun tearDown() {
		testEnv.close()
	}

	@Test
	fun `executes every tool call from one llm step`() {
		val input = AgentTurnInput(chatId = 99L, userId = 1L, text = "что на сегодня")
		val stub =
			testEnv.workflowClient.newWorkflowStub(
				WorkoutAgentWorkflow::class.java,
				WorkflowOptions
					.newBuilder()
					.setTaskQueue(TemporalConstants.TASK_QUEUE)
					.setWorkflowId("test-agent-parallel-tools")
					.build(),
			)
		WorkflowClient.start(stub::handleTurn, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals(2, toolCalls.size)
		assertTrue(
			toolCalls.containsAll(
				listOf(
					ToolCallInput(99L, "getDaySummaries", """{"days":"2026-06-07"}""", toolCallId = "tc-1"),
					ToolCallInput(99L, "listExercises", "{}", toolCallId = "tc-2"),
				),
			),
		)
	}
}
