package dev.myutils.api.temporal.agent

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.agent.memory.AgentUserFactsService
import dev.myutils.api.infra.observability.AgentMetrics
import dev.myutils.api.infra.observability.GenAiTracing
import dev.myutils.api.telegram.AgentStatusMessenger
import dev.myutils.api.telegram.TelegramMessenger
import io.micrometer.core.instrument.simple.SimpleMeterRegistry
import io.opentelemetry.api.OpenTelemetry
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.whenever
import org.springframework.beans.factory.ObjectProvider

class WorkoutToolActivitiesImplTest {
	private val toolsService = mock<WorkoutToolsService>()
	private val openTelemetryProvider = mock<ObjectProvider<OpenTelemetry>>()
	private val genAiTracing =
		run {
			whenever(openTelemetryProvider.getIfAvailable(any())).thenReturn(OpenTelemetry.noop())
			GenAiTracing(openTelemetryProvider)
		}
	private val activities =
		WorkoutToolActivitiesImpl(
			toolsService = toolsService,
			objectMapper = ObjectMapper(),
			genAiTracing = genAiTracing,
		)

	private fun activitiesWithRealTools(): WorkoutToolActivitiesImpl {
		val messengerProvider = mock<ObjectProvider<TelegramMessenger>>()
		whenever(messengerProvider.getIfAvailable()).thenReturn(null)
		val userFactsProvider = mock<ObjectProvider<AgentUserFactsService>>()
		whenever(userFactsProvider.getIfAvailable()).thenReturn(null)
		val agentStatusProvider = mock<ObjectProvider<AgentStatusMessenger>>()
		whenever(agentStatusProvider.getIfAvailable()).thenReturn(null)
		val realTools =
			WorkoutToolsService(
				workoutBotFacade = mock(),
				healthBodyWeightService = mock(),
				temporalNotificationFacade = mock(),
				agentMetrics = AgentMetrics(SimpleMeterRegistry()),
				telegramMessenger = messengerProvider,
				userFacts = userFactsProvider,
				agentStatus = agentStatusProvider,
			)
		return WorkoutToolActivitiesImpl(realTools, ObjectMapper(), genAiTracing)
	}

	@Test
	fun `repairs json syntax and delegates valid days`() {
		whenever(toolsService.runTool(any(), any(), any())).thenReturn("сводки")
		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "getDaySummaries",
					argumentsJson = """{"days": "2026-06-09", "from": 20226-06-09, "to": 20226-06-09}""",
				),
			)

		assertFalse(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("сводки"))
	}

	@Test
	fun `invalid date values return failure feedback for llm`() {
		val result =
			activitiesWithRealTools().executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "getDaySummaries",
					argumentsJson = """{"days":"20226-06-09"}""",
				),
			)

		assertTrue(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Неверная дата"))
		assertTrue(result.contains("hint"))
	}

	@Test
	fun `unparseable tool arguments return failure feedback for llm`() {
		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "getDaySummaries",
					argumentsJson = """{"days":""",
				),
			)

		assertTrue(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Невалидный JSON"))
	}

	@Test
	fun `read only question cannot delete a workout`() {
		whenever(toolsService.runTool(any(), any(), any())).thenReturn("Удалено")

		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "deleteWorkout",
					argumentsJson = """{"exercise_name":"Плечи","performed_on":"2026-07-31"}""",
					userMessage = "Что на этой неделе осталось?",
				),
			)

		assertTrue(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("только чтение"))
	}

	@Test
	fun `explicit delete request can delete a workout`() {
		whenever(toolsService.runTool(any(), any(), any())).thenReturn("Удалено")

		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "deleteWorkout",
					argumentsJson = """{"exercise_name":"Плечи","performed_on":"2026-07-31"}""",
					userMessage = "Удали плечи за вчера",
				),
			)

		assertFalse(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Удалено"))
	}

	@Test
	fun `read only plan cannot log a workout`() {
		whenever(toolsService.runTool(any(), any(), any())).thenReturn("Записано")

		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "logWorkout",
					argumentsJson = """{"exercise_name":"Присед","notation":"60 3*10/12"}""",
					userMessage = "Что завтра делать?",
				),
			)

		assertTrue(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("только чтение"))
	}

	@Test
	fun `workout shorthand can log a workout`() {
		whenever(toolsService.runTool(any(), any(), any())).thenReturn("Записано")

		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "logWorkout",
					argumentsJson = """{"exercise_name":"Бабочка","notation":"43 3*8/9"}""",
					userMessage = "43 кг 8/9",
				),
			)

		assertFalse(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Записано"))
	}

	@Test
	fun `body weight value can log body weight`() {
		whenever(toolsService.runTool(any(), any(), any())).thenReturn("Вес записан")

		val result =
			activities.executeTool(
				ToolCallInput(
					chatId = 303179278L,
					toolName = "logBodyWeight",
					argumentsJson = """{"weight_kg":"82.1"}""",
					userMessage = "82.1 кг",
				),
			)

		assertFalse(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Вес записан"))
	}
}
