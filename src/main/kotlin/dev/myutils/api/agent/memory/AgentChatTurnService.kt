package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.langchain4j.data.message.AiMessage
import dev.myutils.api.agent.langchain.ChatMemoryMessageMapper
import dev.myutils.api.agent.langchain.WorkoutLangChain4jAgent
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.TemporalWorkflowService
import dev.myutils.api.temporal.agent.AgentTurnInput
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.web.server.ResponseStatusException

@Service
class AgentChatTurnService(
	private val properties: MyUtilsProperties,
	private val temporalWorkflow: ObjectProvider<TemporalWorkflowService>,
	private val langChainAgent: ObjectProvider<WorkoutLangChain4jAgent>,
	private val conversationStore: ObjectProvider<AgentConversationStore>,
	private val messageRepository: AgentConversationMessageRepository,
	private val objectMapper: ObjectMapper,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun runSyncTurn(
		chatId: Long,
		text: String,
		images: List<String>? = null,
		contextChatId: Long = chatId,
	): AgentMemoryChatTurnResult {
		val trimmed = text.trim()
		val normalizedImages = AgentMessageImages.normalize(images)
		require(AgentMessageImages.hasPayload(trimmed, normalizedImages)) {
			"Нужен текст или хотя бы одно изображение."
		}
		val afterId = messageRepository.maxIdByChatId(chatId)
		val userId = resolveUserId()
		val hasImages = normalizedImages.isNotEmpty()

		if (hasImages) {
			val userMessage =
				AgentMessageImages.toUserMessage(trimmed, normalizedImages)
					?: throw IllegalArgumentException("Не удалось собрать user message.")
			conversationStore.getIfAvailable()?.append(chatId, listOf(userMessage))
				?: persistUserMessage(chatId, trimmed, normalizedImages)
		}

		var executionReply: String? = null
		try {
			val temporal = temporalWorkflow.getIfAvailable()
			if (temporal != null && properties.temporal.enabled) {
				temporal.executeAgentTurn(
					AgentTurnInput(
						chatId = chatId,
						userId = userId,
						text = if (hasImages) "" else trimmed,
						maxToolIterations = AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get(),
						mutationAuthorizationText = trimmed.takeIf { hasImages },
						deliverToTelegram = false,
						contextChatId = contextChatId,
					),
				)
			} else {
				val agent =
					langChainAgent.getIfAvailable()
						?: throw ResponseStatusException(
							HttpStatus.SERVICE_UNAVAILABLE,
							"Агент недоступен (Telegram-бот / OpenRouter не настроен).",
						)
				if (hasImages) {
					executionReply = agent.runFromMemory(
						chatId = chatId,
						mutationAuthorizationText = trimmed,
						contextChatId = contextChatId,
						publishToolStatus = false,
					)
				} else {
					executionReply = agent.run(
						chatId = chatId,
						userMessage = trimmed,
						contextChatId = contextChatId,
						publishToolStatus = false,
					)
				}
			}
		} catch (error: Exception) {
			log.error("Synchronous agent turn failed chatId={}", chatId, error)
			persistAssistantError(chatId)
		}

		var newMessages =
			messageRepository.findByChatIdAndIdGreaterThanOrderByCreatedAtAsc(chatId, afterId)
		var reply = extractAssistantReply(newMessages)
		if (reply == null) {
			reply = executionReply?.takeIf { it.isNotBlank() } ?: "Готово."
			persistAssistantMessage(chatId, reply)
			newMessages =
				messageRepository.findByChatIdAndIdGreaterThanOrderByCreatedAtAsc(chatId, afterId)
		}
		return AgentMemoryChatTurnResult(
			reply = reply,
			messages = newMessages.map { it.toDto() },
		)
	}

	private fun persistUserMessage(
		chatId: Long,
		content: String,
		images: List<String>,
	) {
		val dto =
			ChatMessage(
				role = "user",
				content = content.ifBlank { null },
				images = images,
			)
		messageRepository.save(
			AgentConversationMessage(
				chatId = chatId,
				messageJson = objectMapper.writeValueAsString(dto),
			),
		)
	}

	private fun persistAssistantError(chatId: Long) {
		persistAssistantMessage(chatId, "❌ Не удалось обработать запрос. Попробуй ещё раз.")
	}

	private fun persistAssistantMessage(
		chatId: Long,
		reply: String,
	) {
		conversationStore.getIfAvailable()?.append(chatId, listOf(AiMessage.from(reply)))
			?: messageRepository.save(
				AgentConversationMessage(
					chatId = chatId,
					messageJson =
						objectMapper.writeValueAsString(
							ChatMessage(
								role = "assistant",
								content = reply,
							),
						),
				),
			)
	}

	private fun resolveUserId(): Long {
		val allowed = properties.telegram.allowedUserIdSet()
		return allowed.firstOrNull() ?: 1L
	}

	private fun extractAssistantReply(messages: List<AgentConversationMessage>): String? =
		messages
			.mapNotNull { message ->
				val parsed = runCatching { objectMapper.readValue<ChatMessage>(message.messageJson) }.getOrNull()
				parsed
					?.takeIf { it.role == "assistant" && !it.content.isNullOrBlank() }
					?.content
			}.lastOrNull()

	private fun AgentConversationMessage.toDto(): AgentMemoryMessageDto {
		val parsed = runCatching { objectMapper.readValue<ChatMessage>(messageJson) }.getOrNull()
		return AgentMemoryMessageDto(
			id = id,
			chatId = chatId,
			role = parsed?.role ?: "unknown",
			content = parsed?.content,
			images = parsed?.images?.takeIf { it.isNotEmpty() },
			toolCallId = parsed?.toolCallId,
			toolName = parsed?.name,
			excludedFromContext = excludedFromContext,
			compactedIntoSummaryId = compactedIntoSummaryId,
			isCompacted = isCompacted,
			createdAt = createdAt,
			rawJson = messageJson,
		)
	}
}
