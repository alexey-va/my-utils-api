package dev.myutils.api.agent.memory

import dev.myutils.api.agent.langchain.ChatModelFactory
import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.domain.AgentContextSummary
import dev.myutils.api.domain.AgentContextSummaryRepository
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.properties.AppProperties
import dev.langchain4j.data.message.UserMessage
import dev.langchain4j.model.chat.request.ChatRequest
import org.slf4j.LoggerFactory
import org.springframework.jdbc.core.JdbcTemplate
import org.springframework.scheduling.annotation.Async
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional

@Service
@ConditionalOnTelegramBot
class AgentContextCompactionService(
	private val messageRepository: AgentConversationMessageRepository,
	private val summaryRepository: AgentContextSummaryRepository,
	private val chatModelFactory: ChatModelFactory,
	private val objectMapper: ObjectMapper,
	private val jdbcTemplate: JdbcTemplate,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@Async
	@Transactional
	fun maybeCompactAfterAppend(chatId: Long) {
		try {
			compactAutoLocked(chatId)
		} catch (error: Exception) {
			log.warn("Auto compact failed chatId={}: {}", chatId, error.message)
			throw error
		}
	}

	@Transactional
	fun compactAuto(chatId: Long): CompactResult {
		return compactAutoLocked(chatId)
	}

	private fun compactAutoLocked(chatId: Long): CompactResult {
		lockChatForCompaction(chatId)
		val tailKeep = AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get()
		val threshold = AppProperties.AGENT_MEMORY_COMPACT_THRESHOLD_MESSAGES.get()
		val compactable = loadCompactableMessages(chatId)
		val toCompact =
			CompactionSelection.selectForAutoCompaction(
				compactableOrdered = compactable,
				tailKeep = tailKeep,
				threshold = threshold,
			)
		return runCompaction(chatId, safeCompactionPrefix(compactable, toCompact.size))
	}

	@Transactional
	fun compactManual(
		chatId: Long,
		keepRecent: Int,
	): CompactResult {
		lockChatForCompaction(chatId)
		val compactable = loadCompactableMessages(chatId)
		val toCompact =
			CompactionSelection.selectForAdminCompaction(
				compactableOrdered = compactable,
				keepRecent = keepRecent,
			)
		return runCompaction(chatId, safeCompactionPrefix(compactable, toCompact.size))
	}

	private fun safeCompactionPrefix(
		ordered: List<AgentConversationMessage>,
		selectedCount: Int,
	): List<AgentConversationMessage> {
		val safeCount =
			CompactionSelection.rewindSplitToolTurn(ordered, selectedCount) { row ->
				StoredMessageFilter.roleFromJson(row.messageJson, objectMapper)
			}
		return ordered.take(safeCount)
	}

	private fun loadCompactableMessages(chatId: Long): List<AgentConversationMessage> =
		messageRepository
			.findByChatIdAndExcludedFromContextFalseAndIsCompactedFalseOrderByCreatedAtAsc(chatId)
			.filterNot { StoredMessageFilter.isSystemStored(it, objectMapper) }

	private fun runCompaction(
		chatId: Long,
		toCompact: List<AgentConversationMessage>,
	): CompactResult {
		if (toCompact.isEmpty()) {
			return CompactResult(compacted = false, messageCount = 0, summaryId = null)
		}

		val previousSummary = summaryRepository.findByChatId(chatId)
		val dialogText = formatMessagesForSummary(previousSummary?.summaryText, toCompact)
		val tokensBefore = dialogText.length
		val modelName = compactionModel()
		val summaryText =
			chatModelFactory
				.create(modelName)
				.chat(
					ChatRequest
						.builder()
						.messages(
							listOf(
								UserMessage.from(COMPACT_SYSTEM_PROMPT),
								UserMessage.from(dialogText),
							),
						).build(),
				).aiMessage()
				.text()
				.orEmpty()
				.trim()
		require(summaryText.isNotBlank()) { "Модель вернула пустой summary." }

		val summary =
			if (previousSummary == null) {
				AgentContextSummary(
					chatId = chatId,
					sequence = 1,
					summaryText = summaryText,
					coversMessageIdFrom = toCompact.first().id,
					coversMessageIdTo = toCompact.last().id,
					sourceMessageCount = toCompact.size,
					model = modelName,
					tokensBefore = tokensBefore,
					tokensAfter = summaryText.length,
				)
			} else {
				previousSummary.apply {
					sequence = 1
					this.summaryText = summaryText
					coversMessageIdFrom = minOf(coversMessageIdFrom, toCompact.first().id)
					coversMessageIdTo = maxOf(coversMessageIdTo, toCompact.last().id)
					sourceMessageCount += toCompact.size
					model = modelName
					this.tokensBefore = tokensBefore
					tokensAfter = summaryText.length
				}
			}.let(summaryRepository::save)
		toCompact.forEach { message ->
			message.compactedIntoSummaryId = summary.id
			message.isCompacted = true
			messageRepository.save(message)
		}
		log.info(
			"Compacted chatId={} messages={} rollingSummaryId={}",
			chatId,
			toCompact.size,
			summary.id,
		)
		return CompactResult(compacted = true, messageCount = toCompact.size, summaryId = summary.id)
	}

	private fun lockChatForCompaction(chatId: Long) {
		jdbcTemplate.queryForObject(
			"SELECT pg_advisory_xact_lock(?)",
			{ _, _ -> Unit },
			chatId,
		)
	}

	private fun compactionModel(): String {
		val configured = AppProperties.AGENT_MEMORY_COMPACT_MODEL.get().trim()
		return configured.ifEmpty { AppProperties.OPENROUTER_MODEL.get() }
	}

	private fun formatMessagesForSummary(
		previousSummary: String?,
		messages: List<AgentConversationMessage>,
	): String =
		buildString {
			if (!previousSummary.isNullOrBlank()) {
				appendLine("Текущий накопленный summary:")
				appendLine(previousSummary.trim())
				appendLine()
			}
			appendLine("Новые сообщения для добавления в summary (${messages.size}):")
			messages.forEach { row ->
				appendLine("--- id=${row.id} at=${row.createdAt}")
				appendLine(row.messageJson)
			}
		}

	private companion object {
		val COMPACT_SYSTEM_PROMPT =
			"""
			Обнови единый накопительный summary диалога агента с пользователем на русском.
			Верни один цельный summary: объедини прежний summary с новыми сообщениями, убери повторы
			и замени устаревшие договорённости более свежими.
			Сохрани: темы, решения, числа (веса, даты), просьбы пользователя.
			Не дублируй долгосрочные факты о пользователе (травмы, цели) — они хранятся отдельно.
			Формат: короткие буллеты, без воды.
			""".trimIndent()
	}

	data class CompactResult(
		val compacted: Boolean,
		val messageCount: Int,
		val summaryId: java.util.UUID?,
	)
}
