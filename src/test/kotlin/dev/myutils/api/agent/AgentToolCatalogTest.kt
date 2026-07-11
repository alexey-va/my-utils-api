package dev.myutils.api.agent

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

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
}
