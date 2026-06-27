package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.agent.memory.AgentChatTurnService
import dev.myutils.api.domain.AgentContextSummary
import dev.myutils.api.domain.AgentContextSummaryRepository
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.domain.AgentUserFact
import dev.myutils.api.domain.AgentUserFactRepository
import dev.myutils.api.infra.openrouter.ChatMessage
import org.springframework.beans.factory.ObjectProvider
import org.springframework.data.domain.PageRequest
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.Instant
import java.util.UUID

@Service
class AgentMemoryAdminService(
	private val messageRepository: AgentConversationMessageRepository,
	private val summaryRepository: AgentContextSummaryRepository,
	private val factRepository: AgentUserFactRepository,
	private val compactionService: ObjectProvider<AgentContextCompactionService>,
	private val chatTurnService: AgentChatTurnService,
	private val memoryAssembler: ObjectProvider<AgentMemoryAssembler>,
	private val objectMapper: ObjectMapper,
) {
	fun listChats(): List<AgentMemoryChatSummary> {
		val chatIds =
			(messageRepository.findDistinctChatIds() + factRepository.findDistinctChatIds())
				.distinct()
				.sorted()
		return chatIds.map { chatId -> chatSummary(chatId) }
	}

	fun getChat(chatId: Long): AgentMemoryChatDetail {
		val summaries = summaryRepository.findByChatIdOrderBySequenceAsc(chatId)
		val facts = factRepository.findByChatIdOrderByUpdatedAtDesc(chatId)
		val recentContext =
			memoryAssembler.getIfAvailable()?.loadContextForLlm(chatId)?.size
				?: messageRepository.countByChatId(chatId).toInt()
		return AgentMemoryChatDetail(
			chatId = chatId,
			stats = chatSummary(chatId),
			summaries = summaries.map { it.toDto() },
			facts = facts.map { it.toDto() },
			recentContextMessageCount = recentContext,
			compaction = compactionPreview(chatId),
		)
	}

	fun listMessages(
		chatId: Long,
		beforeId: Long?,
		limit: Int,
	): AgentMemoryMessagePage {
		val pageSize = limit.coerceIn(1, 200)
		val rows =
			messageRepository.findHistory(
				chatId,
				beforeId,
				PageRequest.of(0, pageSize),
			)
		val nextBeforeId = rows.lastOrNull()?.id
		return AgentMemoryMessagePage(
			messages = rows.map { it.toDto() },
			nextBeforeId = if (rows.size == pageSize) nextBeforeId else null,
		)
	}

	@Transactional
	fun createFact(
		chatId: Long,
		content: String,
		confidence: Double? = null,
	): AgentMemoryFactDto {
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Факт не может быть пустым." }
		val fact =
			factRepository.save(
				AgentUserFact(
					chatId = chatId,
					content = trimmed,
					confidence = FactConfidence.normalizeOrDefault(confidence, FactConfidence.ADMIN_DEFAULT),
				),
			)
		return fact.toDto()
	}

	@Transactional
	fun updateFact(
		id: UUID,
		content: String,
		confidence: Double? = null,
	): AgentMemoryFactDto {
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Факт не может быть пустым." }
		val fact =
			factRepository.findById(id).orElseThrow { IllegalArgumentException("Факт не найден.") }
		fact.content = trimmed
		if (confidence != null) {
			fact.confidence = FactConfidence.normalize(confidence)
		}
		fact.updatedAt = Instant.now()
		return factRepository.save(fact).toDto()
	}

	@Transactional
	fun deleteFact(id: UUID) {
		factRepository.deleteById(id)
	}

	@Transactional
	fun deleteSummary(id: UUID) {
		if (!summaryRepository.existsById(id)) {
			throw IllegalArgumentException("Summary не найден.")
		}
		messageRepository.detachFromSummary(id)
		summaryRepository.deleteById(id)
	}

	@Transactional
	fun updateMessageExcluded(
		messageId: Long,
		excluded: Boolean,
	): AgentMemoryMessageDto {
		val message =
			messageRepository.findById(messageId).orElseThrow { IllegalArgumentException("Сообщение не найдено.") }
		message.excludedFromContext = excluded
		return messageRepository.save(message).toDto()
	}

	@Transactional
	fun deleteMessage(messageId: Long) {
		messageRepository.deleteById(messageId)
	}

	@Transactional
	fun appendMessage(
		chatId: Long,
		role: String,
		content: String,
	): AgentMemoryMessageDto {
		val trimmed = content.trim()
		require(trimmed.isNotEmpty()) { "Текст сообщения не может быть пустым." }
		val normalizedRole = role.trim().lowercase()
		require(normalizedRole in ALLOWED_APPEND_ROLES) {
			"role должен быть user, assistant или system."
		}
		val dto = ChatMessage(role = normalizedRole, content = trimmed)
		val saved =
			messageRepository.save(
				AgentConversationMessage(
					chatId = chatId,
					messageJson = objectMapper.writeValueAsString(dto),
				),
			)
		compactionService.getIfAvailable()?.maybeCompactAfterAppend(chatId)
		return saved.toDto()
	}

	fun simulateChatTurn(
		chatId: Long,
		content: String,
	): AgentMemoryChatTurnResult = chatTurnService.runSyncTurn(chatId, content)

	fun compact(
		chatId: Long,
		keepRecent: Int,
	): AgentMemoryCompactResult {
		if (compactionService.getIfAvailable() == null) {
			throw ResponseStatusException(
				HttpStatus.SERVICE_UNAVAILABLE,
				"Compaction недоступен (Telegram-бот / OpenRouter не настроен).",
			)
		}
		val normalizedKeepRecent = keepRecent.coerceAtLeast(0)
		val result = compactionService.getObject().compactManual(chatId, normalizedKeepRecent)
		if (result.compacted) {
			return AgentMemoryCompactResult(
				compacted = true,
				messageCount = result.messageCount,
				summaryId = result.summaryId,
				reason = null,
			)
		}
		val preview = compactionPreview(chatId)
		val reason =
			when {
				!preview.compactionAvailable -> "unavailable"
				preview.compactableCount <= normalizedKeepRecent -> "nothing_to_compact"
				else -> "nothing_to_compact"
			}
		return AgentMemoryCompactResult(
			compacted = false,
			messageCount = 0,
			summaryId = null,
			reason = reason,
		)
	}

	@Transactional
	fun clearDialog(chatId: Long) {
		summaryRepository.deleteByChatId(chatId)
		messageRepository.deleteByChatId(chatId)
	}

	private fun compactionPreview(chatId: Long): AgentMemoryCompactionPreview {
		val compactionAvailable = compactionService.getIfAvailable() != null
		val compactableCount =
			messageRepository
				.findByChatIdAndExcludedFromContextFalseAndIsCompactedFalseOrderByCreatedAtAsc(chatId)
				.count { !StoredMessageFilter.isSystemStored(it, objectMapper) }
		return AgentMemoryCompactionPreview(
			compactionAvailable = compactionAvailable,
			compactableCount = compactableCount,
		)
	}

	private fun chatSummary(chatId: Long): AgentMemoryChatSummary {
		val messageCount = messageRepository.countByChatId(chatId)
		val factCount = factRepository.findByChatIdOrderByUpdatedAtDesc(chatId).size.toLong()
		val summaryCount = summaryRepository.findByChatIdOrderBySequenceAsc(chatId).size.toLong()
		val lastActivity =
			messageRepository
				.findByChatIdOrderByCreatedAtDesc(chatId, PageRequest.of(0, 1))
				.firstOrNull()
				?.createdAt
		return AgentMemoryChatSummary(
			chatId = chatId,
			messageCount = messageCount,
			factCount = factCount,
			summaryCount = summaryCount,
			lastActivityAt = lastActivity,
		)
	}

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

	private fun AgentContextSummary.toDto(): AgentMemorySummaryDto =
		AgentMemorySummaryDto(
			id = id,
			sequence = sequence,
			summaryText = summaryText,
			coversMessageIdFrom = coversMessageIdFrom,
			coversMessageIdTo = coversMessageIdTo,
			sourceMessageCount = sourceMessageCount,
			model = model,
			tokensBefore = tokensBefore,
			tokensAfter = tokensAfter,
			createdAt = createdAt,
		)

	private fun AgentUserFact.toDto(): AgentMemoryFactDto =
		AgentMemoryFactDto(
			id = id,
			chatId = chatId,
			content = content,
			confidence = confidence,
			createdAt = createdAt,
			updatedAt = updatedAt,
		)

	private companion object {
		val ALLOWED_APPEND_ROLES = setOf("user", "assistant", "system")
	}
}

data class AgentMemoryChatSummary(
	val chatId: Long,
	val messageCount: Long,
	val factCount: Long,
	val summaryCount: Long,
	val lastActivityAt: Instant?,
)

data class AgentMemoryChatDetail(
	val chatId: Long,
	val stats: AgentMemoryChatSummary,
	val summaries: List<AgentMemorySummaryDto>,
	val facts: List<AgentMemoryFactDto>,
	val recentContextMessageCount: Int,
	val compaction: AgentMemoryCompactionPreview,
)

data class AgentMemoryCompactionPreview(
	val compactionAvailable: Boolean,
	val compactableCount: Int,
)

data class AgentMemoryMessagePage(
	val messages: List<AgentMemoryMessageDto>,
	val nextBeforeId: Long?,
)

data class AgentMemoryMessageDto(
	val id: Long,
	val chatId: Long,
	val role: String,
	val content: String?,
	val toolCallId: String?,
	val toolName: String?,
	val excludedFromContext: Boolean,
	val compactedIntoSummaryId: UUID?,
	val isCompacted: Boolean,
	val createdAt: Instant,
	val rawJson: String,
)

data class AgentMemorySummaryDto(
	val id: UUID,
	val sequence: Int,
	val summaryText: String,
	val coversMessageIdFrom: Long,
	val coversMessageIdTo: Long,
	val sourceMessageCount: Int,
	val model: String?,
	val tokensBefore: Int?,
	val tokensAfter: Int?,
	val createdAt: Instant,
)

data class AgentMemoryFactDto(
	val id: UUID,
	val chatId: Long,
	val content: String,
	val confidence: Double,
	val createdAt: Instant,
	val updatedAt: Instant,
)

data class AgentMemoryCompactResult(
	val compacted: Boolean,
	val messageCount: Int,
	val summaryId: UUID?,
	val reason: String? = null,
)

data class AgentMemoryChatTurnResult(
	val reply: String,
	val messages: List<AgentMemoryMessageDto>,
)
