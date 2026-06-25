package dev.myutils.api.agent

import dev.myutils.api.agent.langchain.WorkoutLangChain4jAgent
import dev.myutils.api.agent.memory.AgentConversationStore
import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.InMemoryTelegramMessenger
import dev.myutils.api.testkit.impl.StubChatModelFactory
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.test.context.TestPropertySource

class WorkoutAgentIntegrationTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var service: WorkoutAgentService

	@Autowired
	private lateinit var agent: WorkoutLangChain4jAgent

	@Autowired
	private lateinit var telegram: InMemoryTelegramMessenger

	@Autowired
	private lateinit var chatModelFactory: StubChatModelFactory

	@Autowired
	private lateinit var conversationStore: AgentConversationStore

	@BeforeEach
	fun resetFakes() {
		telegram.clear()
		chatModelFactory.model.resetResponses("Записал подход.", "Второй ответ.")
	}

	@Test
	fun `start sends welcome without calling LLM`() =
		runBlocking {
			service.handleMessage(chatId = 1L, userId = 1L, text = "/start")

			assertTrue(telegram.messagesFor(1L).any { it.text.contains("Тренер по дневнику") })
			assertTrue(chatModelFactory.model.requests.isEmpty())
		}

	@Test
	fun `user message goes through stub model and telegram`() =
		runBlocking {
			service.handleMessage(chatId = 2L, userId = 2L, text = "жим 70 3*10")

			assertTrue(chatModelFactory.model.requests.isNotEmpty())
			assertTrue(telegram.messagesFor(2L).any { it.text.contains("Записал подход") })
			assertTrue(telegram.typingCount(2L) >= 1)
		}

	@Test
	fun `agent returns stub reply without HTTP`() {
		chatModelFactory.model.resetResponses("План: жим 60 кг, 3×10.")

		val reply = agent.run(chatId = 42L, userMessage = "что на сегодня")

		assertEquals("План: жим 60 кг, 3×10.", reply)
		assertTrue(chatModelFactory.model.requests.isNotEmpty())
	}

	@Test
	fun `agent trims empty model reply to default`() {
		chatModelFactory.model.resetResponses("   ")

		val reply = agent.run(chatId = 43L, userMessage = "привет")

		assertEquals("Готово.", reply)
	}

	@Test
	fun `agent persists chat memory across turns`() {
		agent.run(chatId = 99L, userMessage = "первый")
		agent.run(chatId = 99L, userMessage = "второй")

		assertTrue(conversationStore.loadRecent(99L).size >= 2)
		assertEquals(2, chatModelFactory.model.requests.size)
	}
}

@TestPropertySource(properties = ["myutils.telegram.allowed-user-ids=999"])
class WorkoutAgentAccessIntegrationTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var service: WorkoutAgentService

	@Autowired
	private lateinit var telegram: InMemoryTelegramMessenger

	@Test
	fun `rejects unknown user`() =
		runBlocking {
			service.handleMessage(chatId = 5L, userId = 1L, text = "привет")

			assertTrue(telegram.messagesFor(5L).any { it.text.contains("нет доступа") })
		}
}
