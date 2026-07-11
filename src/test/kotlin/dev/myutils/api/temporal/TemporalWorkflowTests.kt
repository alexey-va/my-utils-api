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
import dev.myutils.api.temporal.notification.NotificationWorkflowInput
import dev.myutils.api.temporal.notification.TelegramNotificationWorkflow
import dev.myutils.api.temporal.notification.TelegramNotificationWorkflowImpl
import dev.myutils.api.temporal.reminder.ReminderWorkflowInput
import dev.myutils.api.temporal.telegram.TelegramActivities
import io.temporal.client.WorkflowClient
import io.temporal.client.WorkflowClientOptions
import io.temporal.client.WorkflowOptions
import io.temporal.common.converter.DataConverter
import io.temporal.testing.TestEnvironmentOptions
import io.temporal.testing.TestWorkflowEnvironment
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Nested
import org.junit.jupiter.api.Test
import java.time.Duration

class TemporalWorkflowTests {
	private lateinit var testEnv: TestWorkflowEnvironment

	private val llmSteps = mutableListOf<AgentLlmStepInput>()
	private val toolCalls = mutableListOf<ToolCallInput>()
	private val sentMessages = mutableListOf<Pair<Long, String>>()
	private var llmStepCount = 0

	@BeforeEach
	fun setUp() {
		llmSteps.clear()
		toolCalls.clear()
		sentMessages.clear()
		llmStepCount = 0
		testEnv = TemporalTestSupport.create()
		val worker = testEnv.newWorker(TemporalConstants.TASK_QUEUE)
		worker.registerWorkflowImplementationTypes(
			WorkoutAgentWorkflowImpl::class.java,
			TelegramNotificationWorkflowImpl::class.java,
		)
		worker.registerActivitiesImplementations(
			stubAgentActivities(),
			stubToolActivities(),
			stubTelegramActivities(),
		)
		testEnv.start()
	}

	private fun stubAgentActivities(): WorkoutAgentActivities =
		object : WorkoutAgentActivities {
			override fun resolvePrelude(input: AgentTurnInput): AgentPreludeResult =
				if (input.text == "/start") {
					AgentPreludeResult(AgentPreludeResult.Kind.REPLY, "welcome")
				} else {
					AgentPreludeResult(AgentPreludeResult.Kind.CONTINUE)
				}

			override fun llmStep(input: AgentLlmStepInput): AgentLlmStepResult {
				llmSteps.add(input)
				llmStepCount++
				return if (llmStepCount == 1) {
					AgentLlmStepResult(
						toolCalls =
							listOf(
								ToolCallDto("tc-1", "list_exercises", "{}"),
							),
					)
				} else {
					AgentLlmStepResult(reply = "stub reply")
				}
			}

			override fun recordToolResults(input: RecordToolResultsInput) = Unit

			override fun recordTurnMetrics(input: AgentTurnMetricsInput) = Unit
		}

	private fun stubToolActivities(): WorkoutToolActivities =
		object : WorkoutToolActivities {
			override fun executeTool(input: ToolCallInput): String {
				toolCalls.add(input)
				return "ok"
			}
		}

	private fun stubTelegramActivities(): TelegramActivities =
		object : TelegramActivities {
			override fun sendMessage(
				chatId: Long,
				text: String,
			) {
				sentMessages.add(chatId to text)
			}

			override fun updateAgentStatus(
				chatId: Long,
				text: String,
			) = Unit

			override fun completeAgentStatus(chatId: Long) = Unit

			override fun failAgentStatus(
				chatId: Long,
				text: String,
			) {
				sentMessages.add(chatId to text)
			}
		}

	@AfterEach
	fun tearDown() {
		testEnv.close()
	}

	@Test
	fun `agent workflow runs tool activity between llm steps`() {
		val input = AgentTurnInput(chatId = 42L, userId = 1L, text = "что на сегодня")
		WorkflowClient.start(agentStub("test-agent-turn")::handleTurn, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals(2, llmSteps.size)
		assertEquals("что на сегодня", llmSteps.first().userMessage)
		assertEquals(null, llmSteps[1].userMessage)
		assertEquals(listOf(ToolCallInput(42L, "list_exercises", "{}", toolCallId = "tc-1")), toolCalls)
		assertEquals(listOf(42L to "stub reply"), sentMessages)
	}

	@Test
	fun `agent workflow forwards start reply from prelude`() {
		val input = AgentTurnInput(chatId = 10L, userId = 1L, text = "/start")
		WorkflowClient.start(agentStub("test-agent-start")::handleTurn, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals(listOf(10L to "welcome"), sentMessages)
	}

	@Test
	fun `notification workflow delivers immediately when deliverAt is now`() {
		val input =
			NotificationWorkflowInput(
				chatId = 3L,
				message = "сейчас",
				deliverAtEpochMillis = testEnv.currentTimeMillis(),
			)
		WorkflowClient.start(notificationStub("test-notify-now")::deliver, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals(listOf(3L to "сейчас"), sentMessages)
	}

	@Test
	fun `notification workflow skips virtual time until deliverAt`() {
		val deliverAt = testEnv.currentTimeMillis() + Duration.ofSeconds(10).toMillis()
		val input =
			NotificationWorkflowInput(
				chatId = 7L,
				message = "пора тренироваться",
				deliverAtEpochMillis = deliverAt,
			)
		WorkflowClient.start(notificationStub("test-notify-delayed")::deliver, input)
		testEnv.sleep(Duration.ofSeconds(5))
		assertTrue(sentMessages.isEmpty())

		testEnv.sleep(Duration.ofSeconds(10))
		assertEquals(listOf(7L to "пора тренироваться"), sentMessages)
	}

	private fun agentStub(workflowId: String): WorkoutAgentWorkflow =
		testEnv.workflowClient.newWorkflowStub(
			WorkoutAgentWorkflow::class.java,
			workflowOptions(workflowId),
		)

	private fun notificationStub(workflowId: String): TelegramNotificationWorkflow =
		testEnv.workflowClient.newWorkflowStub(
			TelegramNotificationWorkflow::class.java,
			workflowOptions(workflowId),
		)

	private fun workflowOptions(workflowId: String): WorkflowOptions =
		WorkflowOptions
			.newBuilder()
			.setTaskQueue(TemporalConstants.TASK_QUEUE)
			.setWorkflowId(workflowId)
			.build()

	@Nested
	inner class PayloadSerialization {
		private val converter: DataConverter = TemporalDataConverterConfiguration().temporalDataConverter()

		@Test
		fun `round-trips AgentTurnInput`() {
			val input = AgentTurnInput(chatId = 42L, userId = 1L, text = "жим")
			assertRoundTrip(input, AgentTurnInput::class.java)
		}

		@Test
		fun `round-trips ReminderWorkflowInput`() {
			val input = ReminderWorkflowInput(chatId = 303179278L, hour = 20, minute = 30)
			assertRoundTrip(input, ReminderWorkflowInput::class.java)
		}

		@Test
		fun `round-trips NotificationWorkflowInput`() {
			val input =
				NotificationWorkflowInput(
					chatId = 1L,
					message = "test",
					deliverAtEpochMillis = 1_700_000_000_000L,
				)
			assertRoundTrip(input, NotificationWorkflowInput::class.java)
		}

		private fun <T> assertRoundTrip(
			value: T,
			type: Class<T>,
		) {
			val payload = converter.toPayload(value).orElseThrow()
			val restored = converter.fromPayload(payload, type, type)
			assertEquals(value, restored)
		}
	}
}

internal object TemporalTestSupport {
	fun create(): TestWorkflowEnvironment {
		val dataConverter = TemporalDataConverterConfiguration().temporalDataConverter()
		val options =
			TestEnvironmentOptions
				.newBuilder()
				.setWorkflowClientOptions(
					WorkflowClientOptions
						.newBuilder()
						.setDataConverter(dataConverter)
						.build(),
				).build()
		return TestWorkflowEnvironment.newInstance(options)
	}
}
