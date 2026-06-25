package dev.myutils.api.agent.memory

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import dev.myutils.api.domain.AgentConversationMessage

class CompactionSelectionTest {
	@Test
	fun `keeps tail and respects threshold`() {
		val messages = (1L..51L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForCompaction(
				compactableOrdered = messages,
				tailKeep = 10,
				threshold = 40,
				force = false,
			)
		assertEquals(41, selected.size)
		assertEquals(1L, selected.first().id)
		assertEquals(41L, selected.last().id)
	}

	@Test
	fun `skips when below threshold`() {
		val messages = (1L..20L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForCompaction(
				compactableOrdered = messages,
				tailKeep = 10,
				threshold = 40,
				force = false,
			)
		assertTrue(selected.isEmpty())
	}

	@Test
	fun `force compacts short history keeping recent tail`() {
		val messages = (1L..4L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForCompaction(
				compactableOrdered = messages,
				tailKeep = 10,
				threshold = 40,
				force = true,
			)
		assertEquals(1, selected.size)
		assertEquals(1L, selected.first().id)
	}

	@Test
	fun `skips single compactable message`() {
		val selected =
			CompactionSelection.selectForCompaction(
				compactableOrdered = listOf(message(1)),
				tailKeep = 10,
				threshold = 40,
				force = true,
			)
		assertTrue(selected.isEmpty())
	}

	private fun message(id: Long): AgentConversationMessage =
		AgentConversationMessage(
			id = id,
			chatId = 1L,
			messageJson = """{"role":"user","content":"hi"}""",
		)
}
