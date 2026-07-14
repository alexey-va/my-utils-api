package dev.myutils.api.agent.langchain

import dev.langchain4j.agent.tool.ToolExecutionRequest
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ImageContent
import dev.langchain4j.data.message.TextContent
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.infra.openrouter.ToolCall
import dev.myutils.api.infra.openrouter.ToolCallFunction
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ChatMemoryMessageMapperTest {
	@Test
	fun `round-trips assistant tool calls`() {
		val original =
			AiMessage.from(
				listOf(
					ToolExecutionRequest
						.builder()
						.id("tc-1")
						.name("getDaySummaries")
						.arguments("""{"days":"2026-06-07"}""")
						.build(),
				),
			)

		val dto = ChatMemoryMessageMapper.toDto(original)!!
		val restored = ChatMemoryMessageMapper.toLangChain(dto) as AiMessage

		assertEquals("assistant", dto.role)
		assertEquals(1, dto.toolCalls?.size)
		assertEquals("tc-1", dto.toolCalls?.first()?.id)
		assertEquals("getDaySummaries", restored.toolExecutionRequests().first().name())
	}

	@Test
	fun `round-trips tool execution result`() {
		val original = ToolExecutionResultMessage.from("tc-1", "getDaySummaries", """{"days":[]}""")

		val dto = ChatMemoryMessageMapper.toDto(original)!!
		val restored = ChatMemoryMessageMapper.toLangChain(dto) as ToolExecutionResultMessage

		assertEquals("tool", dto.role)
		assertEquals("tc-1", dto.toolCallId)
		assertEquals("getDaySummaries", dto.name)
		assertEquals("tc-1", restored.id())
		assertEquals("getDaySummaries", restored.toolName())
		assertTrue(restored.text().contains("days"))
	}

	@Test
	fun `round-trips stored chat history with tool loop`() {
		val history =
			listOf(
				ChatMessage(role = "user", content = "что на сегодня"),
				ChatMessage(
					role = "assistant",
					toolCalls =
						listOf(
							ToolCall(
								id = "tc-1",
								function =
									ToolCallFunction(
										name = "getDaySummaries",
										arguments = """{"days":"2026-06-07"}""",
									),
							),
						),
				),
				ChatMessage(
					role = "tool",
					toolCallId = "tc-1",
					name = "getDaySummaries",
					content = """{"summary":"ok"}""",
				),
			)

		val langChain = history.mapNotNull { ChatMemoryMessageMapper.toLangChain(it) }
		val back = langChain.mapNotNull { ChatMemoryMessageMapper.toDto(it) }

		assertEquals(3, back.size)
		assertEquals("tool", back.last().role)
		assertEquals("tc-1", back.last().toolCallId)
		assertEquals("getDaySummaries", back.last().name)
		assertTrue(back[1].toolCalls?.isNotEmpty() == true)
	}

	@Test
	fun `round-trips plain user message`() {
		val original = UserMessage.from("жим 70")

		val dto = ChatMemoryMessageMapper.toDto(original)!!
		val restored = ChatMemoryMessageMapper.toLangChain(dto) as UserMessage

		assertEquals("user", dto.role)
		assertEquals("жим 70", restored.singleText())
	}

	@Test
	fun `round-trips user message with image`() {
		val dataUrl = "data:image/png;base64,abc"
		val original = UserMessage.from(TextContent.from("смотри"), ImageContent.from(dataUrl))

		val dto = ChatMemoryMessageMapper.toDto(original)!!
		val restored = ChatMemoryMessageMapper.toLangChain(dto) as UserMessage

		assertEquals("смотри", dto.content)
		assertEquals(listOf(dataUrl), dto.images)
		assertEquals("смотри", userMessageText(restored))
		assertTrue(restored.contents().any { it is ImageContent })
	}

	@Test
	fun `restores stored chat message with images field`() {
		val dto =
			ChatMessage(
				role = "user",
				content = "фото",
				images = listOf("data:image/jpeg;base64,xyz"),
			)

		val restored = ChatMemoryMessageMapper.toLangChain(dto) as UserMessage

		assertEquals("фото", userMessageText(restored))
		assertTrue(restored.contents().any { it is ImageContent })
	}

	private fun userMessageText(message: UserMessage): String? {
		if (message.hasSingleText()) {
			return message.singleText().trim().ifBlank { null }
		}
		return message
			.contents()
			.mapNotNull { part ->
				if (part is TextContent) {
					part.text().trim()
				} else {
					null
				}
			}.joinToString("\n")
			.trim()
			.ifBlank { null }
	}
}
