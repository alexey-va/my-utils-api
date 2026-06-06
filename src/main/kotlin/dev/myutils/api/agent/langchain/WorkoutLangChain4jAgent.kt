package dev.myutils.api.agent.langchain

import dev.myutils.api.agent.WorkoutAgentContextBuilder
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.agent.AgentLlmStepInput
import dev.myutils.api.temporal.agent.AgentLlmStepResult
import dev.myutils.api.temporal.agent.RecordToolResultsInput
import dev.myutils.api.temporal.agent.ToolCallDto
import dev.myutils.api.infra.util.LogPreview
import dev.langchain4j.agent.tool.ToolSpecifications
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import dev.langchain4j.memory.chat.MessageWindowChatMemory
import dev.langchain4j.model.chat.request.ChatRequest
import dev.langchain4j.service.AiServices
import dev.langchain4j.store.memory.chat.ChatMemoryStore
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class WorkoutLangChain4jAgent(
	private val properties: MyUtilsProperties,
	private val chatModelFactory: ChatModelFactory,
	private val memoryStore: ChatMemoryStore,
	private val contextBuilder: WorkoutAgentContextBuilder,
	private val toolsService: WorkoutToolsService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	/** Прямой путь без Temporal — LangChain4j сам крутит tool loop. */
	fun run(
		chatId: Long,
		userMessage: String,
	): String {
		val tools = WorkoutLangChainTools.create(chatId, toolsService, properties)
		val assistant =
			AiServices
				.builder(WorkoutAgentAssistant::class.java)
				.chatModel(chatModelFactory.create())
				.tools(tools)
				.chatMemoryProvider { id ->
					MessageWindowChatMemory
						.builder()
						.id(id)
						.maxMessages(MEMORY_WINDOW)
						.chatMemoryStore(memoryStore)
						.build()
				}.maxSequentialToolsInvocations(AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get())
				.build()

		val reply =
			assistant.chat(
				chatId,
				userMessage,
				AppProperties.AGENT_SYSTEM_PROMPT.get(),
				contextBuilder.buildSnapshot(),
			)
		log.info("LangChain4j agent chatId={} reply={}", chatId, LogPreview.of(reply))
		return reply.trim().ifEmpty { "Готово." }
	}

	/** Один шаг LLM для Temporal workflow (без выполнения tools внутри activity). */
	fun llmStep(input: AgentLlmStepInput): AgentLlmStepResult {
		val chatId = input.chatId
		val tools = WorkoutLangChainTools.create(chatId, toolsService, properties)
		val toolSpecs = ToolSpecifications.toolSpecificationsFrom(tools)

		val requestMessages = buildLlmMessages(chatId, input.userMessage)
		val response =
			chatModelFactory.create().chat(
				ChatRequest
					.builder()
					.messages(requestMessages)
					.toolSpecifications(toolSpecs)
					.build(),
			)
		val aiMessage = response.aiMessage()

		appendToMemory(chatId, buildMemoryAppend(input.userMessage, aiMessage))

		log.info(
			"LangChain4j llmStep chatId={} tools={} reply={}",
			chatId,
			aiMessage.toolExecutionRequests().size,
			LogPreview.of(aiMessage.text().orEmpty()),
		)

		return AgentLlmStepResult(
			reply = aiMessage.text().orEmpty(),
			toolCalls =
				aiMessage.toolExecutionRequests().map { req ->
					ToolCallDto(
						id = req.id(),
						name = req.name(),
						argumentsJson = req.arguments(),
					)
				},
		)
	}

	fun recordToolResults(input: RecordToolResultsInput) {
		val append =
			input.results.map { result ->
				ToolExecutionResultMessage.from(
					result.toolCallId,
					result.toolName,
					result.result,
				)
			}
		appendToMemory(input.chatId, append)
	}

	private fun buildLlmMessages(
		chatId: Long,
		userMessage: String?,
	): List<LcChatMessage> {
		val messages = mutableListOf<LcChatMessage>()
		messages.add(SystemMessage.from(systemContext()))
		messages.addAll(memoryStore.getMessages(chatId))
		if (!userMessage.isNullOrBlank()) {
			messages.add(UserMessage.from(userMessage))
		}
		return messages
	}

	private fun buildMemoryAppend(
		userMessage: String?,
		aiMessage: AiMessage,
	): List<LcChatMessage> {
		val append = mutableListOf<LcChatMessage>()
		if (!userMessage.isNullOrBlank()) {
			append.add(UserMessage.from(userMessage))
		}
		append.add(aiMessage)
		return append
	}

	private fun appendToMemory(
		chatId: Long,
		append: List<LcChatMessage>,
	) {
		if (append.isEmpty()) {
			return
		}
		val stored = memoryStore.getMessages(chatId).toMutableList()
		stored.addAll(append)
		memoryStore.updateMessages(chatId, trimMemory(stored))
	}

	private fun systemContext(): String =
		"""
		${AppProperties.AGENT_SYSTEM_PROMPT.get()}

		${contextBuilder.buildSnapshot()}
		""".trimIndent()

	private fun trimMemory(messages: List<LcChatMessage>): List<LcChatMessage> =
		if (messages.size <= MEMORY_WINDOW) {
			messages
		} else {
			messages.takeLast(MEMORY_WINDOW)
		}

	private companion object {
		const val MEMORY_WINDOW = 24
	}
}
