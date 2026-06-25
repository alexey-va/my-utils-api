package dev.myutils.api.agent.langchain

import dev.myutils.api.agent.WorkoutAgentContextBuilder
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.agent.memory.AgentConversationStore
import dev.myutils.api.agent.memory.AgentMemoryAssembler
import dev.myutils.api.agent.memory.AgentUserFactsService
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.agent.AgentLlmStepInput
import dev.myutils.api.temporal.agent.AgentLlmStepResult
import dev.myutils.api.temporal.agent.RecordToolResultsInput
import dev.myutils.api.temporal.agent.ToolCallDto
import dev.myutils.api.infra.util.LlmRequestLog
import dev.myutils.api.infra.util.LogPreview
import dev.myutils.api.temporal.logging.TemporalActivityLog
import dev.langchain4j.agent.tool.ToolSpecifications
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import dev.langchain4j.memory.chat.MessageWindowChatMemory
import dev.langchain4j.model.chat.request.ChatRequest
import dev.langchain4j.service.AiServices
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class WorkoutLangChain4jAgent(
	private val properties: MyUtilsProperties,
	private val chatModelFactory: ChatModelFactory,
	private val conversationStore: AgentConversationStore,
	private val memoryAssembler: AgentMemoryAssembler,
	private val userFacts: AgentUserFactsService,
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
		val memoryLimit = AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get()
		val assistant =
			AiServices
				.builder(WorkoutAgentAssistant::class.java)
				.chatModel(chatModelFactory.create())
				.tools(tools)
				.chatMemoryProvider { id ->
					MessageWindowChatMemory
						.builder()
						.id(id)
						.maxMessages(memoryLimit)
						.chatMemoryStore(conversationStore)
						.build()
				}.maxSequentialToolsInvocations(AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get())
				.build()

		val reply =
			assistant.chat(
				chatId,
				userMessage,
				AppProperties.AGENT_SYSTEM_PROMPT.get(),
				contextBuilder.buildSnapshot(),
				userFacts.formatForPrompt(chatId),
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
		TemporalActivityLog
			.enrich(
				log
					.atInfo()
					.setMessage("LLM request")
					.addKeyValue("chatId", chatId)
					.addKeyValue("messageCount", requestMessages.size)
					.addKeyValue("toolSpecCount", toolSpecs.size)
					.addKeyValue("userMessage", LogPreview.of(input.userMessage))
					.addKeyValue("messages", LlmRequestLog.summarize(requestMessages)),
			).log()

		val response =
			chatModelFactory.create().chat(
				ChatRequest
					.builder()
					.messages(requestMessages)
					.toolSpecifications(toolSpecs)
					.build(),
			)
		val aiMessage = response.aiMessage()

		conversationStore.append(chatId, buildMemoryAppend(input.userMessage, aiMessage))

		TemporalActivityLog
			.enrich(
				log
					.atInfo()
					.setMessage("LLM response")
					.addKeyValue("chatId", chatId)
					.addKeyValue("toolCallCount", aiMessage.toolExecutionRequests().size)
					.addKeyValue("reply", LogPreview.of(aiMessage.text().orEmpty()))
					.addKeyValue(
						"toolCalls",
						aiMessage.toolExecutionRequests().map { req ->
							"${req.name()}(${req.id()} args=${LogPreview.of(req.arguments(), 120)})"
						},
					),
			).log()

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
		TemporalActivityLog
			.enrich(
				log
					.atInfo()
					.setMessage("Tool results to memory")
					.addKeyValue("chatId", input.chatId)
					.addKeyValue(
						"results",
						input.results.map { result ->
							"${result.toolName}(${result.toolCallId}): ${LogPreview.of(result.result, 200)}"
						},
					),
			).log()
		conversationStore.append(input.chatId, append)
		TemporalActivityLog
			.enrich(
				log
					.atInfo()
					.setMessage("Memory after tool results")
					.addKeyValue("chatId", input.chatId)
					.addKeyValue("messageCount", conversationStore.loadRecent(input.chatId).size)
					.addKeyValue("messages", LlmRequestLog.summarize(conversationStore.loadRecent(input.chatId))),
			).log()
	}

	private fun buildLlmMessages(
		chatId: Long,
		userMessage: String?,
	): List<LcChatMessage> {
		val messages = mutableListOf<LcChatMessage>()
		messages.add(SystemMessage.from(systemContext(chatId)))
		messages.addAll(memoryAssembler.loadContextForLlm(chatId))
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

	private fun systemContext(chatId: Long): String =
		"""
		${AppProperties.AGENT_SYSTEM_PROMPT.get()}

		${contextBuilder.buildSnapshot()}

		${userFacts.formatForPrompt(chatId)}
		""".trimIndent()
}
