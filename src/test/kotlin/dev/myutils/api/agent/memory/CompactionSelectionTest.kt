package dev.myutils.api.agent.memory

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import dev.myutils.api.domain.AgentConversationMessage

class CompactionSelectionTest {
	@Test
	fun `auto keeps tail and respects threshold`() {
		val messages = (1L..51L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForAutoCompaction(
				compactableOrdered = messages,
				tailKeep = 10,
				threshold = 40,
			)
		assertEquals(41, selected.size)
		assertEquals(1L, selected.first().id)
		assertEquals(41L, selected.last().id)
	}

	@Test
	fun `auto skips when below threshold`() {
		val messages = (1L..20L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForAutoCompaction(
				compactableOrdered = messages,
				tailKeep = 10,
				threshold = 40,
			)
		assertTrue(selected.isEmpty())
	}

	@Test
	fun `admin compacts all when keepRecent is zero`() {
		val messages = (1L..4L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForAdminCompaction(
				compactableOrdered = messages,
				keepRecent = 0,
			)
		assertEquals(4, selected.size)
		assertEquals(1L, selected.first().id)
		assertEquals(4L, selected.last().id)
	}

	@Test
	fun `admin compacts single message when keepRecent is zero`() {
		val selected =
			CompactionSelection.selectForAdminCompaction(
				compactableOrdered = listOf(message(1)),
				keepRecent = 0,
			)
		assertEquals(1, selected.size)
		assertEquals(1L, selected.first().id)
	}

	@Test
	fun `admin keeps recent tail`() {
		val messages = (1L..10L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForAdminCompaction(
				compactableOrdered = messages,
				keepRecent = 3,
			)
		assertEquals(7, selected.size)
		assertEquals(1L, selected.first().id)
		assertEquals(7L, selected.last().id)
	}

	@Test
	fun `admin skips when nothing exceeds keepRecent`() {
		val messages = (1L..3L).map { id -> message(id) }
		val selected =
			CompactionSelection.selectForAdminCompaction(
				compactableOrdered = messages,
				keepRecent = 3,
			)
		assertTrue(selected.isEmpty())
	}

	@Test
	fun `rewinds boundary before assistant when it would split tool results`() {
		val roles = listOf("user", "assistant", "tool", "tool", "user")

		assertEquals(1, CompactionSelection.rewindSplitToolTurn(roles, 2) { it })
		assertEquals(1, CompactionSelection.rewindSplitToolTurn(roles, 3) { it })
		assertEquals(4, CompactionSelection.rewindSplitToolTurn(roles, 4) { it })
	}

	private fun message(id: Long): AgentConversationMessage =
		AgentConversationMessage(
			id = id,
			chatId = 1L,
			messageJson = """{"role":"user","content":"hi"}""",
		)
}
