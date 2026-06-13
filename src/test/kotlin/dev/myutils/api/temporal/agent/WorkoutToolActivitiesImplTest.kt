package dev.myutils.api.temporal.agent

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.infra.observability.AgentMetrics
import dev.myutils.api.telegram.TelegramMessenger
import io.micrometer.core.instrument.simple.SimpleMeterRegistry
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.any
import org.mockito.kotlin.mock
import org.mockito.kotlin.whenever
import org.springframework.beans.factory.ObjectProvider

class WorkoutToolActivitiesImplTest {
	private val toolsService = mock<WorkoutToolsService>()
	private val activities =
		WorkoutToolActivitiesImpl(
			toolsService = toolsService,
			objectMapper = ObjectMapper(),
		)

	private fun activitiesWithRealTools(): WorkoutToolActivitiesImpl {
		val messengerProvider = mock<ObjectProvider<TelegramMessenger>>()
		whenever(messengerProvider.getIfAvailable()).thenReturn(null)
		val realTools =
			WorkoutToolsService(
				workoutBotFacade = mock(),
				temporalNotificationFacade = mock(),
				agentMetrics = AgentMetrics(SimpleMeterRegistry()),
				telegramMessenger = messengerProvider,
			)
		return WorkoutToolActivitiesImpl(realTools, ObjectMapper())
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
}
