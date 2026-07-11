package dev.myutils.api.agent.memory

import dev.langchain4j.agent.tool.ToolExecutionRequest
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class AgentMemorySanitizerTest {
	@Test
	fun `drops orphan assistant tool call before next user message`() {
		val messages =
			listOf(
				UserMessage.from("первый"),
				AiMessage.from(
					listOf(
						ToolExecutionRequest.builder().id("a1").name("logWorkout").arguments("{}").build(),
					),
				),
				UserMessage.from("второй"),
			)

		val sanitized = AgentMemorySanitizer.dropIncompleteToolTurns(messages)

		assertEquals(listOf(UserMessage.from("первый"), UserMessage.from("второй")), sanitized)
	}

	@Test
	fun `keeps completed tool turn`() {
		val messages =
			listOf(
				UserMessage.from("запиши"),
				AiMessage.from(
					listOf(
						ToolExecutionRequest.builder().id("a1").name("logWorkout").arguments("{}").build(),
					),
				),
				ToolExecutionResultMessage.from("a1", "logWorkout", "ok"),
			)

		val sanitized = AgentMemorySanitizer.dropIncompleteToolTurns(messages)

		assertEquals(messages, sanitized)
	}
}
