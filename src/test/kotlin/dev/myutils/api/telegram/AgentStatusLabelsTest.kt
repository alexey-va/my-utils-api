package dev.myutils.api.telegram

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class AgentStatusLabelsTest {
	@Test
	fun `collapses parallel calls of same tool`() {
		val label =
			AgentStatusLabels.toolsRunning(
				listOf("logWorkout", "log_workout", "logWorkout"),
			)
		assertEquals("Записываю в дневник…", label)
	}

	@Test
	fun `counts distinct tools`() {
		val label =
			AgentStatusLabels.toolsRunning(
				listOf("logWorkout", "getDays", "logWorkout"),
			)
		assertEquals("Выполняю 2 действия…", label)
	}

	@Test
	fun `chart tool has dedicated status`() {
		assertEquals("Строю график прогресса…", AgentStatusLabels.toolRunning("send_progress_chart"))
	}
}
