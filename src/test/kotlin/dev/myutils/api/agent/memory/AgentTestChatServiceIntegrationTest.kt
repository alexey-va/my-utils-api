package dev.myutils.api.agent.memory

import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.domain.AgentTestSandboxStateRepository
import dev.myutils.api.domain.AgentTestChatRepository
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.service.WorkoutService
import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.InMemoryTelegramMessenger
import dev.myutils.api.testkit.impl.StubChatModelFactory
import dev.langchain4j.agent.tool.ToolExecutionRequest
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.SystemMessage
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.HttpStatus
import org.springframework.transaction.annotation.Transactional
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
	private lateinit var properties: MyUtilsProperties

	@Autowired
	private lateinit var chatModelFactory: StubChatModelFactory

	@Autowired
	private lateinit var workoutService: WorkoutService

	@Autowired
	private lateinit var telegram: InMemoryTelegramMessenger

	@Autowired
	private lateinit var sandboxStates: AgentTestSandboxStateRepository

	@Autowired
	private lateinit var tools: WorkoutToolsService

	@Test
	fun `create allocates isolated safe memory ids and list returns newest first`() {
		val first = service.create("Первый тест")
		val second = service.create("Второй тест")

		try {
			assertTrue(first.memoryChatId in -9_000_000_000_000_000L..-8_000_000_000_000_000L)
			assertNotEquals(first.memoryChatId, second.memoryChatId)
			assertTrue(first.sandboxed)
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
	fun `delete clears isolated dialog and sandbox state`() {
		val created = service.create("Удаляемый")
		messages.save(
			AgentConversationMessage(
				chatId = created.memoryChatId,
				messageJson = """{"role":"user","content":"test"}""",
			),
		)

		service.delete(created.id)

		assertTrue(repository.findById(created.id).isEmpty)
		assertEquals(0, messages.countByChatId(created.memoryChatId))
		assertTrue(sandboxStates.findById(created.memoryChatId).isEmpty)
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

	@Test
	@Transactional
	fun `sandbox mutations never touch real workout data and clear starts fresh`() {
		val realExercisesBefore = workoutService.listExercises().map { it.id to it.name }
		val created = service.create("Sandbox mutation")
		val sandboxExercise = "Sandbox ${UUID.randomUUID().toString().take(8)}"
		chatModelFactory.model.resetMessages(
			toolCall(
				id = "tc-create",
				name = "createExercise",
				arguments = """{"name":"$sandboxExercise","muscle_group":"legs"}""",
			),
			AiMessage.from("Создано в sandbox."),
		)

		service.sendMessage(created.id, "Создай упражнение $sandboxExercise для ног", null)

		assertEquals(realExercisesBefore, workoutService.listExercises().map { it.id to it.name })
		val systemPrompt =
			chatModelFactory.model.requests
				.first()
				.messages()
				.filterIsInstance<SystemMessage>()
				.joinToString("\n") { it.text() }
		assertTrue(systemPrompt.contains("ИЗОЛИРОВАННЫЙ SANDBOX"))

		chatModelFactory.model.resetMessages(
			toolCall(id = "tc-list", name = "listExercises", arguments = "{}"),
			AiMessage.from("Проверил sandbox."),
		)
		val listed = service.sendMessage(created.id, "Покажи упражнения", null)
		assertTrue(listed.messages.single { it.role == "tool" }.content.orEmpty().contains(sandboxExercise))

		service.clearMessages(created.id)
		chatModelFactory.model.resetMessages(
			toolCall(id = "tc-list-empty", name = "listExercises", arguments = "{}"),
			AiMessage.from("Sandbox пуст."),
		)
		val afterClear = service.sendMessage(created.id, "Покажи упражнения", null)
		assertTrue(afterClear.messages.single { it.role == "tool" }.content.orEmpty().contains("пока нет"))
	}

	@Test
	@Transactional
	fun `sandbox delivery tools never call telegram`() {
		telegram.clear()
		val realChatId = properties.telegram.allowedUserIdSet().firstOrNull() ?: 1L
		val created = service.create("Sandbox delivery")
		chatModelFactory.model.resetMessages(
			toolCall(
				id = "tc-send",
				name = "sendRichMessage",
				arguments = """{"text":"sandbox only","buttons":null}""",
			),
		)

		val result = service.sendMessage(created.id, "Отправь сообщение sandbox only", null)

		assertTrue(result.messages.single { it.role == "tool" }.content.orEmpty().contains("SANDBOX"))
		assertTrue(telegram.messagesFor(realChatId).isEmpty())
	}

	@Test
	@Transactional
	fun `reserved sandbox id fails closed when its state is missing`() {
		val created = service.create("Missing sandbox state")
		sandboxStates.deleteById(created.memoryChatId)
		sandboxStates.flush()

		assertThrows(IllegalArgumentException::class.java) {
			tools.runDirectTool(
				name = "listExercises",
				chatId = created.memoryChatId,
				args = emptyMap(),
				publishStatus = false,
			)
		}
	}

	private fun toolCall(
		id: String,
		name: String,
		arguments: String,
	): AiMessage =
		AiMessage.from(
			listOf(
				ToolExecutionRequest
					.builder()
					.id(id)
					.name(name)
					.arguments(arguments)
					.build(),
			),
		)
}
