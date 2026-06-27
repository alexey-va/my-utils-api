package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.agent.langchain.WorkoutLangChain4jAgent
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.TemporalWorkflowService
import dev.myutils.api.temporal.agent.AgentTurnInput
import org.springframework.beans.factory.ObjectProvider
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.web.server.ResponseStatusException

@Service
class AgentChatTurnService(
	private val properties: MyUtilsProperties,
	private val temporalWorkflow: ObjectProvider<TemporalWorkflowService>,
	private val langChainAgent: ObjectProvider<WorkoutLangChain4jAgent>,
	private val messageRepository: AgentConversationMessageRepository,
	private val objectMapper: ObjectMapper,
) {
	fun runSyncTurn(
		chatId: Long,
		text: String,
	): AgentMemoryChatTurnResult {
		val trimmed = text.trim()
		require(trimmed.isNotEmpty()) { "Текст сообщения не может быть пустым." }
		val afterId = messageRepository.maxIdByChatId(chatId)
		val userId = resolveUserId()

		val temporal = temporalWorkflow.getIfAvailable()
		if (temporal != null && properties.temporal.enabled) {
			temporal.executeAgentTurn(
				AgentTurnInput(
					chatId = chatId,
					userId = userId,
					text = trimmed,
					maxToolIterations = AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get(),
					deliverToTelegram = false,
				),
			)
		} else {
			val agent =
				langChainAgent.getIfAvailable()
					?: throw ResponseStatusException(
						HttpStatus.SERVICE_UNAVAILABLE,
						"Агент недоступен (Telegram-бот / OpenRouter не настроен).",
					)
			agent.run(chatId, trimmed)
		}

		val newMessages =
			messageRepository.findByChatIdAndIdGreaterThanOrderByCreatedAtAsc(chatId, afterId)
		val reply = extractAssistantReply(newMessages) ?: "Готово."
		return AgentMemoryChatTurnResult(
			reply = reply,
			messages = newMessages.map { it.toDto() },
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
