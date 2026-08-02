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
import dev.myutils.api.temporal.report.WeeklyHealthReportActivities
import dev.myutils.api.temporal.report.WeeklyHealthReportActivityInput
import dev.myutils.api.temporal.report.WeeklyHealthReportInput
import dev.myutils.api.temporal.report.WeeklyHealthReportWorkflow
import dev.myutils.api.temporal.report.WeeklyHealthReportWorkflowImpl
import dev.myutils.api.temporal.report.nextSaturdayNoon
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
	private val generatedReports = mutableListOf<WeeklyHealthReportActivityInput>()
	private var llmStepCount = 0
	private var llmReply = "stub reply"

	@BeforeEach
	fun setUp() {
		llmSteps.clear()
		toolCalls.clear()
		sentMessages.clear()
		generatedReports.clear()
		llmStepCount = 0
		llmReply = "stub reply"
		testEnv = TemporalTestSupport.create()
		val worker = testEnv.newWorker(TemporalConstants.TASK_QUEUE)
		worker.registerWorkflowImplementationTypes(
			WorkoutAgentWorkflowImpl::class.java,
			TelegramNotificationWorkflowImpl::class.java,
			WeeklyHealthReportWorkflowImpl::class.java,
		)
		worker.registerActivitiesImplementations(
			stubAgentActivities(),
			stubToolActivities(),
			stubTelegramActivities(),
			stubWeeklyHealthReportActivities(),
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
					AgentLlmStepResult(reply = llmReply)
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
				sentMessages.add(chatId to text)
			}
		}

	private fun stubWeeklyHealthReportActivities(): WeeklyHealthReportActivities =
		object : WeeklyHealthReportActivities {
			override fun generateAndSend(input: WeeklyHealthReportActivityInput) {
				generatedReports.add(input)
			}
		}

	@AfterEach
	fun tearDown() {
		testEnv.close()
	}

	@Test
	fun `agent workflow keeps test memory id while tools use real user context`() {
		val input =
			AgentTurnInput(
				chatId = -9_000_000_000_000_000L,
				contextChatId = 42L,
				userId = 1L,
				text = "что на сегодня",
				deliverToTelegram = false,
			)
		WorkflowClient.start(agentStub("test-agent-turn")::handleTurn, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals(2, llmSteps.size)
		assertEquals("что на сегодня", llmSteps.first().userMessage)
		assertEquals(-9_000_000_000_000_000L, llmSteps.first().chatId)
		assertEquals(42L, llmSteps.first().contextChatId)
		assertEquals(null, llmSteps[1].userMessage)
		assertEquals(
			listOf(
				ToolCallInput(
					chatId = -9_000_000_000_000_000L,
					contextChatId = 42L,
					toolName = "list_exercises",
					argumentsJson = "{}",
					toolCallId = "tc-1",
					userMessage = "что на сегодня",
				),
			),
			toolCalls,
		)
		assertTrue(sentMessages.isEmpty())
	}

	@Test
	fun `agent workflow keeps image text only for mutation authorization`() {
		val input =
			AgentTurnInput(
				chatId = 43L,
				userId = 1L,
				text = "",
				mutationAuthorizationText = "Запиши тренировку с изображения",
				deliverToTelegram = false,
			)
		WorkflowClient.start(agentStub("test-agent-image-authorization")::handleTurn, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals("", llmSteps.first().userMessage)
		assertEquals("Запиши тренировку с изображения", toolCalls.single().userMessage)
		assertTrue(sentMessages.isEmpty())
	}

	@Test
	fun `agent workflow normalizes model reply before telegram delivery`() {
		llmReply = "### План\nБля, **завтра** — отдых."
		val input = AgentTurnInput(chatId = 44L, userId = 1L, text = "что завтра")
		WorkflowClient.start(agentStub("test-agent-reply-normalization")::handleTurn, input)
		testEnv.sleep(Duration.ofSeconds(5))

		assertEquals(listOf(44L to "<b>План</b>\nБля, <b>завтра</b> — отдых."), sentMessages)
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

	@Test
	fun `weekly health report runs at next Saturday noon in configured zone`() {
		val now = testEnv.currentTimeMillis()
		val nextRun = nextSaturdayNoon("Europe/Moscow", now)
		val untilRun =
			Duration.ofMillis(nextRun.toInstant().toEpochMilli() - now)
		WorkflowClient.start(
			weeklyReportStub("test-weekly-health-report")::run,
			WeeklyHealthReportInput(chatId = 42L, zoneId = "Europe/Moscow", lookbackDays = 90),
		)

		testEnv.sleep(untilRun.minusSeconds(1))
		assertTrue(generatedReports.isEmpty())
		testEnv.sleep(Duration.ofSeconds(2))

		assertEquals(1, generatedReports.size)
		assertEquals(42L, generatedReports.single().chatId)
		assertEquals(90, generatedReports.single().lookbackDays)
		assertEquals(nextRun.toLocalDate().toString(), generatedReports.single().reportDate)
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

	private fun weeklyReportStub(workflowId: String): WeeklyHealthReportWorkflow =
		testEnv.workflowClient.newWorkflowStub(
			WeeklyHealthReportWorkflow::class.java,
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
		fun `default Temporal converter can replay ReminderWorkflowInput`() {
			val defaultConverter = io.temporal.common.converter.DefaultDataConverter.newDefaultInstance()
			val input = ReminderWorkflowInput(chatId = 303179278L, hour = 20, minute = 30)
			val payload = defaultConverter.toPayload(input).orElseThrow()

			assertEquals(
				input,
				defaultConverter.fromPayload(payload, ReminderWorkflowInput::class.java, ReminderWorkflowInput::class.java),
			)
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

		@Test
		fun `round-trips WeeklyHealthReportInput`() {
			assertRoundTrip(
				WeeklyHealthReportInput(
					chatId = 303179278L,
					zoneId = "Europe/Moscow",
					lookbackDays = 90,
				),
				WeeklyHealthReportInput::class.java,
			)
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
