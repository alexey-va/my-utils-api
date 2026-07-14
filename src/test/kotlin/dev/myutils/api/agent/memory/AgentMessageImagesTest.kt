package dev.myutils.api.agent.memory

import dev.langchain4j.data.message.ImageContent
import dev.langchain4j.data.message.TextContent
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class AgentMessageImagesTest {
	@Test
	fun `builds multimodal user message`() {
		val message =
			AgentMessageImages.toUserMessage(
				content = "что на фото",
				images = listOf("data:image/png;base64,abc"),
			)!!

		assertEquals(2, message.contents().size)
		assertTrue(message.contents()[0] is TextContent)
		assertTrue(message.contents()[1] is ImageContent)
	}

	@Test
	fun `rejects non data urls`() {
		assertThrows<IllegalArgumentException> {
			AgentMessageImages.normalize(listOf("https://example.com/a.png"))
		}
	}
}
