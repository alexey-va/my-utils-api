package dev.myutils.api.testkit.impl

import com.pengrad.telegrambot.model.request.Keyboard
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage
import dev.langchain4j.model.chat.ChatModel
import dev.langchain4j.model.chat.request.ChatRequest
import dev.langchain4j.model.chat.response.ChatResponse
import dev.langchain4j.store.memory.chat.ChatMemoryStore
import dev.myutils.api.agent.langchain.ChatModelFactory
import dev.myutils.api.telegram.TelegramMessenger
import org.springframework.boot.test.context.TestConfiguration
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Primary
import java.util.concurrent.ConcurrentHashMap

// --- fakes ---

class InMemoryTelegramMessenger : TelegramMessenger {
	private val messages = ConcurrentHashMap<Long, MutableList<SentMessage>>()
	private val typingCounts = ConcurrentHashMap<Long, Int>()
	private val callbackAnswers = mutableListOf<CallbackAnswer>()

	data class SentMessage(
		val text: String,
		val replyMarkup: Keyboard? = null,
	)

	data class CallbackAnswer(
		val callbackQueryId: String,
		val text: String?,
	)

	override fun sendHtmlMessage(
		chatId: Long,
		text: String,
		replyMarkup: Keyboard?,
	) {
		messages.computeIfAbsent(chatId) { mutableListOf() }.add(SentMessage(text, replyMarkup))
	}

	override fun sendTyping(chatId: Long) {
		typingCounts.merge(chatId, 1, Int::plus)
	}

	override fun answerCallback(
		callbackQueryId: String,
		text: String?,
	) {
		callbackAnswers.add(CallbackAnswer(callbackQueryId, text))
	}

	fun messagesFor(chatId: Long): List<SentMessage> = messages[chatId]?.toList() ?: emptyList()

	fun typingCount(chatId: Long): Int = typingCounts[chatId] ?: 0

	fun clear() {
		messages.clear()
		typingCounts.clear()
		callbackAnswers.clear()
	}
}

class InMemoryChatMemoryStore : ChatMemoryStore {
	private val store = ConcurrentHashMap<Any, List<ChatMessage>>()

	override fun getMessages(memoryId: Any): List<ChatMessage> = store[memoryId] ?: emptyList()

	override fun updateMessages(
		memoryId: Any,
		messages: List<ChatMessage>,
	) {
		store[memoryId] = messages.toList()
	}

	override fun deleteMessages(memoryId: Any) {
		store.remove(memoryId)
	}

	fun messageCount(memoryId: Long): Int = store[memoryId]?.size ?: 0
}

class RecordingChatModel(
	defaultResponse: String = "Тестовый ответ.",
) : ChatModel {
	private val responseQueue = ArrayDeque<String>()

	val requests = mutableListOf<ChatRequest>()

	init {
		responseQueue.addLast(defaultResponse)
	}

	fun resetResponses(vararg responses: String) {
		responseQueue.clear()
		requests.clear()
		responses.forEach { responseQueue.addLast(it) }
		if (responseQueue.isEmpty()) {
			responseQueue.addLast("Тестовый ответ.")
		}
	}

	override fun chat(request: ChatRequest): ChatResponse {
		requests.add(request)
		val text = responseQueue.removeFirstOrNull() ?: "ok"
		return ChatResponse.builder().aiMessage(AiMessage.from(text)).build()
	}
}

class StubChatModelFactory(
	val model: RecordingChatModel = RecordingChatModel(),
) : ChatModelFactory {
	override fun create() = model
}

// --- Spring wiring (imported only for Environment.TESTING) ---

@TestConfiguration
class TestingClientsConfiguration {
	@Bean
	fun stubChatModelFactory(): StubChatModelFactory = StubChatModelFactory()

	@Bean
	@Primary
	fun chatModelFactory(stub: StubChatModelFactory): ChatModelFactory = stub

	@Bean
	fun inMemoryTelegramMessenger(): InMemoryTelegramMessenger = InMemoryTelegramMessenger()

	@Bean
	@Primary
	fun telegramMessenger(messenger: InMemoryTelegramMessenger): TelegramMessenger = messenger

	@Bean
	fun inMemoryChatMemoryStore(): InMemoryChatMemoryStore = InMemoryChatMemoryStore()

	@Bean
	@Primary
	fun chatMemoryStore(store: InMemoryChatMemoryStore): ChatMemoryStore = store
}
