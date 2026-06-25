package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.agent.langchain.ChatMemoryMessageMapper
import dev.myutils.api.agent.langchain.ChatModelFactory
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.domain.AgentContextSummaryRepository
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.properties.AppProperties
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.data.message.SystemMessage
import dev.langchain4j.model.chat.request.ChatRequest
import dev.langchain4j.data.message.UserMessage
import org.slf4j.LoggerFactory
import org.springframework.data.domain.PageRequest
import org.springframework.stereotype.Service

@Service
@ConditionalOnTelegramBot
class AgentMemoryAssembler(
	private val messageRepository: AgentConversationMessageRepository,
	private val summaryRepository: AgentContextSummaryRepository,
	private val objectMapper: ObjectMapper,
) {
	fun loadContextForLlm(chatId: Long): List<LcChatMessage> {
		val summaries = summaryRepository.findByChatIdOrderBySequenceAsc(chatId)
		val summaryMessages =
			summaries.map { summary ->
				SystemMessage.from(
					"""
					[Контекст диалога (сжато, блок ${summary.sequence})]
					${summary.summaryText.trim()}
					""".trimIndent(),
				)
			}
		return summaryMessages + loadRecentRaw(chatId)
	}

	fun loadRecentRaw(
		chatId: Long,
		limit: Int = AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get(),
	): List<LcChatMessage> =
		messageRepository
			.findByChatIdAndExcludedFromContextFalseAndCompactedIntoSummaryIdIsNullOrderByCreatedAtDesc(
				chatId,
				PageRequest.of(0, limit.coerceAtLeast(1)),
			).asReversed()
			.mapNotNull { row -> decode(row.messageJson)?.let { ChatMemoryMessageMapper.toLangChain(it) } }

	private fun decode(raw: String): ChatMessage? =
		runCatching { objectMapper.readValue<ChatMessage>(raw) }.getOrNull()
}
