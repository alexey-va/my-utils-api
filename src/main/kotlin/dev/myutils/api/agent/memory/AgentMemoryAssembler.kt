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
import java.time.ZoneId
import java.time.format.DateTimeFormatter

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
				AgentMemoryContextLabels.historicalSummary(summary.sequence, summary.summaryText)
			}
		return summaryMessages + loadRecentRaw(chatId)
	}

	fun loadRecentRaw(
		chatId: Long,
		limit: Int = AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get(),
	): List<LcChatMessage> =
		messageRepository
			.findByChatIdAndExcludedFromContextFalseAndIsCompactedFalseOrderByCreatedAtDesc(
				chatId,
				PageRequest.of(0, limit.coerceAtLeast(1)),
			).asReversed()
			.mapNotNull { row ->
				decode(row.messageJson)
					?.takeUnless { StoredMessageFilter.isSystemRole(it.role) }
					?.let { AgentMemoryContextLabels.timestampMessage(it, row.createdAt, memoryZone()) }
					?.let { ChatMemoryMessageMapper.toLangChain(it) }
			}.let { AgentMemorySanitizer.dropIncompleteToolTurns(it) }

	private fun memoryZone(): ZoneId = ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get())

	private fun decode(raw: String): ChatMessage? =
		runCatching { objectMapper.readValue<ChatMessage>(raw) }.getOrNull()
}

internal object AgentMemoryContextLabels {
	private val timestampFormat: DateTimeFormatter = DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm")

	fun historicalSummary(
		sequence: Int,
		summaryText: String,
	): SystemMessage =
		SystemMessage.from(
			"""
			[Исторический контекст диалога (сжато, блок $sequence)]
			Это не источник текущей даты, текущей недели или актуального состояния дневника.
			Для «сегодня / вчера / эта неделя / уже сделано / осталось» используй только свежий снимок из основного system-сообщения.
			${summaryText.trim()}
			""".trimIndent(),
		)

	fun timestampMessage(
		message: ChatMessage,
		createdAt: java.time.Instant,
		zoneId: ZoneId,
	): ChatMessage {
		if (message.role.lowercase() !in setOf("user", "assistant") || message.content.isNullOrBlank()) {
			return message
		}
		val localTimestamp = createdAt.atZone(zoneId)
		val timestamp = localTimestamp.format(timestampFormat)
		return message.copy(content = "[Отправлено $timestamp ${zoneId.id}] ${message.content}")
	}
}
