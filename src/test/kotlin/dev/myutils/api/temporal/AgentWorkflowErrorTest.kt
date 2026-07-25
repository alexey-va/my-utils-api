package dev.myutils.api.temporal

import dev.myutils.api.temporal.agent.AgentLlmStepInput
import dev.myutils.api.temporal.agent.AgentPreludeResult
import dev.myutils.api.temporal.agent.AgentTurnInput
import dev.myutils.api.temporal.agent.AgentTurnMetricsInput
import dev.myutils.api.temporal.agent.RecordToolResultsInput
import dev.myutils.api.temporal.agent.WorkoutAgentActivities
import dev.myutils.api.temporal.agent.WorkoutAgentWorkflow
import dev.myutils.api.temporal.agent.WorkoutAgentWorkflowImpl
import dev.myutils.api.temporal.agent.WorkoutToolActivities
import dev.myutils.api.temporal.telegram.TelegramActivities
import io.temporal.client.WorkflowOptions
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test

class AgentWorkflowErrorTest {
	private lateinit var testEnv: io.temporal.testing.TestWorkflowEnvironment
	private val sentMessages = mutableListOf<String>()

	@BeforeEach
	fun setUp() {
		sentMessages.clear()
		testEnv = TemporalTestSupport.create()
		val worker = testEnv.newWorker(TemporalConstants.TASK_QUEUE)
		worker.registerWorkflowImplementationTypes(WorkoutAgentWorkflowImpl::class.java)
		worker.registerActivitiesImplementations(
			object : WorkoutAgentActivities {
				override fun resolvePrelude(input: AgentTurnInput): AgentPreludeResult =
					AgentPreludeResult(AgentPreludeResult.Kind.CONTINUE)

				override fun llmStep(input: AgentLlmStepInput): Nothing =
					throw IllegalStateException("LLM unavailable")

				override fun recordToolResults(input: RecordToolResultsInput) = Unit

				override fun recordTurnMetrics(input: AgentTurnMetricsInput) = Unit
			},
			object : WorkoutToolActivities {
				override fun executeTool(input: dev.myutils.api.temporal.agent.ToolCallInput): String = "ok"
			},
			object : TelegramActivities {
				override fun sendMessage(
					chatId: Long,
					text: String,
				) {
					sentMessages.add(text)
				}

				override fun agentStatusThinking(
					chatId: Long,
					step: Int,
				) = Unit

				override fun agentStatusTools(
					chatId: Long,
					toolNames: List<String>,
				) = Unit

				override fun agentStatusComposing(chatId: Long) = Unit

				override fun completeAgentStatus(chatId: Long) = Unit

				override fun failAgentStatus(
					chatId: Long,
					text: String,
				) {
					sentMessages.add(text)
				}
			},
		)
		testEnv.start()
	}

	@AfterEach
	fun tearDown() {
		testEnv.close()
	}

	@Test
	fun `notifies user when workflow fails`() {
		val input = AgentTurnInput(chatId = 42L, userId = 1L, text = "что на сегодня")
		val stub =
			testEnv.workflowClient.newWorkflowStub(
				WorkoutAgentWorkflow::class.java,
				WorkflowOptions
					.newBuilder()
					.setTaskQueue(TemporalConstants.TASK_QUEUE)
					.setWorkflowId("test-agent-error-notify")
					.build(),
			)
		stub.handleTurn(input)

		assertTrue(sentMessages.any { it.contains("❌ Не удалось обработать запрос") })
	}
}
