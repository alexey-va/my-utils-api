package dev.myutils.api.agent.memory

import dev.myutils.api.infra.openrouter.ChatMessage
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class ConversationMessageDeltaTest {
	@Test
	fun `appends only new tail when window slides`() {
		val existing =
			listOf(
				ChatMessage(role = "user", content = "a"),
				ChatMessage(role = "assistant", content = "b"),
				ChatMessage(role = "user", content = "c"),
			)
		val incoming =
			listOf(
				ChatMessage(role = "assistant", content = "b"),
				ChatMessage(role = "user", content = "c"),
				ChatMessage(role = "assistant", content = "d"),
			)

		val toAppend = ConversationMessageDelta.findToAppend(existing, incoming)

		assertEquals(listOf(ChatMessage(role = "assistant", content = "d")), toAppend)
	}

	@Test
	fun `appends all messages for empty history`() {
		val incoming =
			listOf(
				ChatMessage(role = "user", content = "hi"),
				ChatMessage(role = "assistant", content = "ok"),
			)

		assertEquals(incoming, ConversationMessageDelta.findToAppend(emptyList(), incoming))
	}
}
