package dev.myutils.api.agent.langchain

import dev.myutils.api.infra.config.MyUtilsProperties
import org.junit.jupiter.api.Assertions.assertNotSame
import org.junit.jupiter.api.Assertions.assertSame
import org.junit.jupiter.api.Test

class LangChain4jChatModelFactoryTest {
	@Test
	fun `reuses one HTTP client backed model per runtime model name`() {
		val factory =
			LangChain4jChatModelFactory(
				MyUtilsProperties(
					openrouter =
						MyUtilsProperties.OpenRouterProperties(
							apiKey = "test-key",
						),
				),
			)

		val first = factory.create("openai/gpt-5.4-mini")
		val second = factory.create("openai/gpt-5.4-mini")
		val other = factory.create("openai/gpt-5.4")

		assertSame(first, second)
		assertNotSame(first, other)
	}
}
