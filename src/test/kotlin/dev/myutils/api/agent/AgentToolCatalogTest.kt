package dev.myutils.api.agent

import org.junit.jupiter.api.Assertions.assertFalse
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
		assertFalse(AgentToolCatalog.isImmediateReturn("log_workout"))
		assertFalse(AgentToolCatalog.isImmediateReturn("getDaySummaries"))
	}
}
