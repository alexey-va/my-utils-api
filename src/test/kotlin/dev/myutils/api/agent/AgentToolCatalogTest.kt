package dev.myutils.api.agent

import dev.langchain4j.agent.tool.ToolSpecifications
import dev.myutils.api.agent.langchain.WorkoutLangChainTools
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock

class AgentToolCatalogTest {
	@Test
	fun `send rich message is immediate return`() {
		assertTrue(AgentToolCatalog.isImmediateReturn("send_rich_message"))
		assertTrue(AgentToolCatalog.isImmediateReturn("sendRichMessage"))
	}

	@Test
	fun `send progress chart is immediate return`() {
		assertTrue(AgentToolCatalog.isImmediateReturn("send_progress_chart"))
		assertTrue(AgentToolCatalog.isImmediateReturn("sendProgressChart"))
	}

	@Test
	fun `estimate 1rm is immediate return`() {
		assertTrue(AgentToolCatalog.isImmediateReturn("estimate_1rm"))
		assertTrue(AgentToolCatalog.isImmediateReturn("estimate1rm"))
	}

	@Test
	fun `estimate 1rm status label`() {
		assertEquals("Считаю 1ПМ…", AgentToolCatalog.statusLabel("estimate_1rm"))
	}

	@Test
	fun `other tools are not immediate return`() {
		assertTrue(!AgentToolCatalog.isImmediateReturn("log_workout"))
		assertTrue(!AgentToolCatalog.isImmediateReturn("getDaySummaries"))
	}

	@Test
	fun `every registered tool has status label`() {
		for (tool in AgentToolCatalog.registeredToolNames()) {
			val label = AgentToolCatalog.statusLabel(tool)
			assertTrue(label.isNotBlank(), "Пустая подпись для $tool")
			assertTrue(!label.startsWith("Выполняю "), "Нет явной подписи для $tool")
		}
	}

	@Test
	fun `send progress chart status label`() {
		assertEquals("Строю график прогресса…", AgentToolCatalog.statusLabel("sendProgressChart"))
	}

	@Test
	fun `agent exposes the complete supported tool set`() {
		val tools =
			WorkoutLangChainTools(
				chatId = 1L,
				toolsService = mock(),
				temporalEnabled = true,
			)
		val names = ToolSpecifications.toolSpecificationsFrom(tools).map { it.name() }.toSet()

		assertEquals(
			setOf(
				"listExercises",
				"createExercise",
				"renameExercise",
				"deleteWorkout",
				"logWorkout",
				"getProgress",
				"getDays",
				"logBodyWeight",
				"getBodyWeight",
				"rememberFact",
				"forgetFact",
				"sendRichMessage",
				"sendProgressChart",
				"estimate1rm",
				"sendNotification",
				"scheduleNotification",
				"cancelNotification",
			),
			names,
		)
	}
}
