package dev.myutils.api.agent.langchain

import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutToolsService
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock
import org.mockito.kotlin.verifyNoInteractions

class WorkoutLangChainToolsTest {
	private val toolsService = mock<WorkoutToolsService>()

	@Test
	fun `direct path blocks an unrequested delete`() {
		val tools = WorkoutLangChainTools(303179278L, toolsService, temporalEnabled = true, "Что осталось на неделе?")

		val result = tools.deleteWorkout("Плечи", "2026-07-31")

		assertTrue(ToolExecutionFeedback.isFailure(result))
		verifyNoInteractions(toolsService)
	}
}
