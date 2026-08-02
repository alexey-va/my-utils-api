package dev.myutils.api.agent.langchain

import dev.myutils.api.agent.AgentReplyNormalizer
import dev.myutils.api.agent.AgentToolCatalog
import dev.myutils.api.agent.AgentToolArgumentsGrounding
import dev.myutils.api.agent.AgentToolMutationPolicy
import dev.myutils.api.agent.ToolArgumentsJsonParser
import dev.myutils.api.agent.ToolExecutionFeedback
import dev.myutils.api.agent.WorkoutAgentContextBuilder
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.agent.memory.AgentConversationStore
import dev.myutils.api.agent.memory.AgentMemoryAssembler
import dev.myutils.api.agent.memory.AgentTestSandboxService
import dev.myutils.api.agent.memory.AgentUserFactsService
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.agent.AgentLlmStepInput
import dev.myutils.api.temporal.agent.AgentLlmStepResult
import dev.myutils.api.temporal.agent.RecordToolResultsInput
import dev.myutils.api.temporal.agent.ToolCallDto
import dev.myutils.api.temporal.agent.ToolCallResultDto
import dev.myutils.api.infra.util.LlmRequestLog
import dev.myutils.api.infra.util.LogPreview
import dev.myutils.api.temporal.logging.TemporalActivityLog
import com.fasterxml.jackson.databind.ObjectMapper
import dev.langchain4j.agent.tool.ToolSpecifications
import dev.langchain4j.data.message.AiMessage
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.data.message.ToolExecutionResultMessage
import dev.langchain4j.data.message.UserMessage
import dev.langchain4j.model.chat.request.ChatRequest
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
	private val sandbox: AgentTestSandboxService,
	private val contextBuilder: WorkoutAgentContextBuilder,
	private val toolsService: WorkoutToolsService,
	private val objectMapper: ObjectMapper,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	/** Прямой путь без Temporal — LangChain4j сам крутит tool loop. */
	fun run(
		chatId: Long,
		userMessage: String,
		contextChatId: Long = chatId,
		publishToolStatus: Boolean = true,
	): String {
		conversationStore.append(chatId, listOf(UserMessage.from(userMessage)))
		val reply =
			runFromMemory(
				chatId = chatId,
				mutationAuthorizationText = userMessage,
				contextChatId = contextChatId,
				publishToolStatus = publishToolStatus,
			)
		log.info(
			"LangChain4j agent chatId={} contextChatId={} reply={}",
			chatId,
			contextChatId,
			LogPreview.of(reply),
		)
		return AgentReplyNormalizer.forTelegram(reply)
	}

	/** Продолжить диалог, когда user message уже в памяти (например с изображением). */
	fun runFromMemory(
		chatId: Long,
		mutationAuthorizationText: String,
		contextChatId: Long = chatId,
		publishToolStatus: Boolean = true,
	): String {
		val maxSteps = AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get().coerceAtLeast(1)
		var userMessage: String? = null
		repeat(maxSteps) {
			val step =
				llmStep(
					AgentLlmStepInput(
						chatId = chatId,
						userMessage = userMessage,
						contextChatId = contextChatId,
					),
				)
			userMessage = null
			if (!step.hasToolCalls) {
				return AgentReplyNormalizer.forTelegram(step.reply)
			}
			val results =
				step.toolCalls.map { call ->
					ToolCallResultDto(
						toolCallId = call.id,
						toolName = call.name,
						result =
							executeToolCall(
								contextChatId,
								call,
								mutationAuthorizationText,
								publishToolStatus,
							),
					)
				}
			recordToolResults(RecordToolResultsInput(chatId = chatId, results = results))
			if (step.toolCalls.isNotEmpty() && step.toolCalls.all { AgentToolCatalog.isImmediateReturn(it.name) }) {
				return "Готово."
			}
		}
		return "Слишком много шагов с инструментами, попробуй короче."
	}

	internal fun executeToolCall(
		chatId: Long,
		call: ToolCallDto,
		mutationAuthorizationText: String,
		publishStatus: Boolean = true,
	): String =
		AgentToolMutationPolicy.denialReason(call.name, mutationAuthorizationText)?.let { reason ->
			ToolExecutionFeedback.failure(
				error = reason,
				hint = "Ответь на вопрос без изменения данных.",
			)
		} ?: try {
			when (val parsed = ToolArgumentsJsonParser.parse(objectMapper, call.argumentsJson)) {
				is ToolArgumentsJsonParser.ParseResult.Ok ->
					toolsService.runDirectTool(
						call.name,
						chatId,
						AgentToolArgumentsGrounding.ground(
							call.name,
							parsed.args,
							mutationAuthorizationText,
						),
						publishStatus,
					)
				is ToolArgumentsJsonParser.ParseResult.Error ->
					ToolExecutionFeedback.failure(
						error = parsed.message,
						hint = "Исправь arguments JSON и вызови инструмент снова. Даты — строки YYYY-MM-DD в кавычках.",
					)
			}
		} catch (ex: Exception) {
			log.warn("Direct tool {} failed chatId={}: {}", call.name, chatId, ex.message, ex)
			ToolExecutionFeedback.failure(
				error = "Ошибка инструмента ${call.name}: ${ex.message ?: "неизвестная ошибка"}",
			)
		}

	/** Один шаг LLM для Temporal workflow (без выполнения tools внутри activity). */
	fun llmStep(input: AgentLlmStepInput): AgentLlmStepResult {
		val chatId = input.chatId
		val contextChatId = input.contextChatId ?: chatId
		val tools = WorkoutLangChainTools.create(contextChatId, toolsService, properties)
		val toolSpecs = ToolSpecifications.toolSpecificationsFrom(tools)

		val requestMessages =
			buildLlmMessages(chatId, contextChatId, input.userMessage).toMutableList()
		if (input.userMessage.isNullOrBlank() && requestMessages.lastOrNull() is ToolExecutionResultMessage) {
			requestMessages.add(
				UserMessage.from("Кратко подведи итог для пользователя на русском по результатам tools."),
			)
		}
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
		val replyText = aiMessage.text().orEmpty()

		conversationStore.append(chatId, buildMemoryAppend(input.userMessage, aiMessage))

		if (AgentReplyGuard.looksInvalidForRussianUser(replyText) && aiMessage.toolExecutionRequests().isEmpty()) {
			TemporalActivityLog
				.enrich(
					log
						.atWarn()
						.setMessage("LLM reply rejected as non-Russian, retrying once")
						.addKeyValue("chatId", chatId)
						.addKeyValue("reply", LogPreview.of(replyText)),
				).log()
			val retryMessages =
				requestMessages +
					UserMessage.from("Ответь только на русском. Кратко подведи итог записей для пользователя.")
			val retryResponse =
				chatModelFactory.create().chat(
					ChatRequest
						.builder()
						.messages(retryMessages)
						.build(),
				)
			val retryAi = retryResponse.aiMessage()
			val retryReply = retryAi.text().orEmpty()
			if (retryReply.isNotBlank() && !AgentReplyGuard.looksInvalidForRussianUser(retryReply)) {
				conversationStore.append(chatId, listOf(retryAi))
				return AgentLlmStepResult(reply = retryReply, toolCalls = emptyList())
			}
		}

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
		contextChatId: Long,
		userMessage: String?,
	): List<LcChatMessage> {
		val messages = mutableListOf<LcChatMessage>()
		messages.add(SystemMessage.from(systemContext(contextChatId)))
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

		${contextBuilder.buildSnapshot(chatId)}

		${if (sandbox.isSandboxChatId(chatId)) sandbox.formatFacts(chatId) else userFacts.formatForPrompt(chatId)}
		""".trimIndent()
}
