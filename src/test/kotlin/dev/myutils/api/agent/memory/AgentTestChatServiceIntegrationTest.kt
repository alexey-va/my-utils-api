package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.domain.AgentTestChatRepository
import dev.myutils.api.domain.AgentUserFactRepository
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.StubChatModelFactory
import dev.langchain4j.agent.tool.ToolExecutionRequest
import dev.langchain4j.data.message.AiMessage
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.HttpStatus
import org.springframework.web.server.ResponseStatusException
import java.util.UUID

class AgentTestChatServiceIntegrationTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var service: AgentTestChatService

	@Autowired
	private lateinit var repository: AgentTestChatRepository

	@Autowired
	private lateinit var messages: AgentConversationMessageRepository

	@Autowired
	private lateinit var facts: AgentUserFactRepository

	@Autowired
	private lateinit var memoryAdmin: AgentMemoryAdminService

	@Autowired
	private lateinit var properties: MyUtilsProperties

	@Autowired
	private lateinit var chatModelFactory: StubChatModelFactory

	@Test
	fun `create allocates isolated safe memory ids and list returns newest first`() {
		val first = service.create("Первый тест")
		val second = service.create("Второй тест")

		try {
			assertTrue(first.memoryChatId in -9_000_000_000_000_000L..-8_000_000_000_000_000L)
			assertNotEquals(first.memoryChatId, second.memoryChatId)
			assertEquals(properties.telegram.allowedUserIdSet().firstOrNull() ?: 1L, first.userContextChatId)
			assertEquals(listOf(second.id, first.id), service.list().take(2).map { it.id })
		} finally {
			service.delete(second.id)
			service.delete(first.id)
		}
	}

	@Test
	fun `rename rejects blank title and unknown chat is not found`() {
		val created = service.create("До")

		try {
			assertEquals("После", service.rename(created.id, "  После  ").title)
			assertThrows(IllegalArgumentException::class.java) {
				service.rename(created.id, "   ")
			}
			val missing =
				assertThrows(ResponseStatusException::class.java) {
					service.get(UUID.randomUUID())
				}
			assertEquals(HttpStatus.NOT_FOUND, missing.statusCode)
		} finally {
			service.delete(created.id)
		}
	}

	@Test
	fun `delete clears isolated dialog but preserves real user facts`() {
		val created = service.create("Удаляемый")
		val fact = memoryAdmin.createFact(created.userContextChatId, "Настоящий пользовательский факт", 1.0)
		messages.save(
			AgentConversationMessage(
				chatId = created.memoryChatId,
				messageJson = """{"role":"user","content":"test"}""",
			),
		)

		service.delete(created.id)

		assertTrue(repository.findById(created.id).isEmpty)
		assertEquals(0, messages.countByChatId(created.memoryChatId))
		assertTrue(facts.findById(fact.id).isPresent)
		memoryAdmin.deleteFact(fact.id)
	}

	@Test
	fun `send runs real tool loop and returns persisted tool round`() {
		val created = service.create("Tool round")
		chatModelFactory.model.resetMessages(
			AiMessage.from(
				listOf(
					ToolExecutionRequest
						.builder()
						.id("tc-list")
						.name("listExercises")
						.arguments("{}")
						.build(),
				),
			),
			AiMessage.from("Список упражнений проверен."),
		)

		try {
			val result = service.sendMessage(created.id, "Покажи список упражнений", null)

			assertEquals("Список упражнений проверен.", result.reply)
			assertEquals("user", result.messages.first().role)
			assertEquals("assistant", result.messages.last().role)
			assertTrue(
				result.messages.any {
					it.role == "assistant" && it.rawJson.contains("\"id\":\"tc-list\"")
				},
			)
			assertTrue(
				result.messages.any {
					it.role == "tool" && it.toolName == "listExercises" && it.toolCallId == "tc-list"
				},
			)
			assertEquals(result.messages.size.toLong(), messages.countByChatId(created.memoryChatId))
		} finally {
			service.delete(created.id)
		}
	}
}
