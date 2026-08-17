package dev.myutils.api.agent.memory

import dev.langchain4j.agent.tool.ToolExecutionRequest
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.SystemMessage
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.domain.AgentTestChatRepository
import dev.myutils.api.domain.AgentTestSandboxStateRepository
import dev.myutils.api.domain.HealthBodyWeightRepository
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.service.WorkoutService
import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.InMemoryTelegramMessenger
import dev.myutils.api.testkit.impl.StubChatModelFactory
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.http.HttpStatus
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.time.ZoneId
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

	@Autowired
	private lateinit var bodyWeights: HealthBodyWeightRepository

	@Autowired
	private lateinit var sandbox: AgentTestSandboxService

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
	fun `failed model turn persists and returns an explicit assistant error`() {
		val created = service.create("Failed turn")
		chatModelFactory.model.resetFailure(IllegalStateException("OpenRouter unavailable"))

		try {
			val result = service.sendMessage(created.id, "что сегодня", null)

			assertEquals("❌ Не удалось обработать запрос. Попробуй ещё раз.", result.reply)
			assertEquals("assistant", result.messages.last().role)
			assertEquals(result.reply, result.messages.last().content)
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
		assertEquals("assistant", result.messages.last().role)
		assertEquals("Готово.", result.messages.last().content)
		assertEquals("Готово.", result.reply)
		assertTrue(telegram.messagesFor(realChatId).isEmpty())
	}

	@Test
	@Transactional
	fun `sandbox body weight belongs to test chat and never touches real weight data`() {
		val realWeightsBefore = bodyWeights.count()
		val created = service.create("Sandbox body weight")
		val recentDate = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get())).minusDays(1).toString()
		chatModelFactory.model.resetMessages(
			toolCall(
				id = "tc-log-weight",
				name = "logBodyWeight",
				arguments = """{"weight_kg":82.4,"date":"$recentDate"}""",
			),
			AiMessage.from("Вес сохранён в тестовом чате."),
		)

		service.sendMessage(created.id, "вес сегодня 82.4", null)

		assertEquals(realWeightsBefore, bodyWeights.count())
		chatModelFactory.model.resetMessages(
			toolCall(
				id = "tc-get-weight",
				name = "getBodyWeight",
				arguments = """{"recent_days":10}""",
			),
			AiMessage.from("Показал тестовый вес."),
		)
		val result = service.sendMessage(created.id, "покажи мой вес", null)
		val toolResult = result.messages.single { it.role == "tool" }.content.orEmpty()
		assertTrue(toolResult.contains("82.4 кг"))
		assertTrue(toolResult.contains(recentDate))
		assertEquals(realWeightsBefore, bodyWeights.count())
	}

	@Test
	@Transactional
	fun `every exposed agent tool executes inside one isolated test user`() {
		val realExercisesBefore = workoutService.listExercises().map { it.id to it.name }
		val realWeightsBefore = bodyWeights.count()
		val created = service.create("All sandbox tools")
		val chatId = created.memoryChatId
		val results = linkedMapOf<String, String>()

		fun execute(
			name: String,
			vararg args: Pair<String, String?>,
		): String =
			sandbox.executeTool(chatId, name, mapOf(*args)).also { result ->
				results[name] = result
				assertTrue(!ToolExecutionFeedback.isFailure(result), "$name failed: $result")
			}

		execute("list_exercises")
		execute("create_exercise", "name" to "Тестовый жим", "muscle_group" to "chest")
		execute(
			"rename_exercise",
			"current_name" to "Тестовый жим",
			"new_name" to "Тестовый жим лёжа",
		)
		execute(
			"log_workout",
			"exercise_name" to "Тестовый жим лёжа",
			"notation" to "80 10/10",
			"date" to "2026-08-02",
		)
		execute("get_exercise_progresses", "exercise" to "Тестовый жим лёжа")
		execute("get_day_summaries", "days" to "2026-08-02")
		execute("log_body_weight", "weight_kg" to "82.4", "date" to "2026-08-02")
		execute("get_body_weight", "recent_days" to "10")
		val remembered = execute("remember_fact", "content" to "Беречь локоть")
		val factId = Regex("""\[([0-9a-f-]+)]""").find(remembered)!!.groupValues[1]
		execute("forget_fact", "fact_id" to factId)
		execute("send_rich_message", "text" to "Только sandbox", "buttons" to "Ок:ok")
		execute("send_progress_chart", "exercise_name" to "Тестовый жим лёжа")
		execute("estimate_1rm", "exercise_name" to "Тестовый жим лёжа")
		execute("send_notification", "message" to "Тестовое уведомление")
		val scheduled =
			execute(
				"schedule_notification",
				"message" to "Тестовое напоминание",
				"deliver_at" to "2026-08-03T09:00:00+03:00",
			)
		val workflowId = Regex("""sandbox-[0-9a-f-]+""").find(scheduled)!!.value
		execute("cancel_notification", "workflow_id" to workflowId)
		execute("delete_workout", "exercise_name" to "Тестовый жим лёжа", "date" to "2026-08-02")

		assertEquals(17, results.size)
		assertEquals(realExercisesBefore, workoutService.listExercises().map { it.id to it.name })
		assertEquals(realWeightsBefore, bodyWeights.count())
		assertTrue(telegram.messagesFor(chatId).isEmpty())
		assertTrue(telegram.photosFor(chatId).isEmpty())
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
