package dev.myutils.api.agent.memory

import dev.myutils.api.agent.langchain.ChatModelFactory
import dev.myutils.api.domain.AgentContextSummary
import dev.myutils.api.domain.AgentContextSummaryRepository
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.properties.AppProperties
import dev.langchain4j.data.message.UserMessage
import dev.langchain4j.model.chat.request.ChatRequest
import org.slf4j.LoggerFactory
import org.springframework.scheduling.annotation.Async
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional

@Service
@ConditionalOnTelegramBot
class AgentContextCompactionService(
	private val messageRepository: AgentConversationMessageRepository,
	private val summaryRepository: AgentContextSummaryRepository,
	private val chatModelFactory: ChatModelFactory,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@Async
	fun maybeCompactAfterAppend(chatId: Long) {
		try {
			compactAuto(chatId)
		} catch (error: Exception) {
			log.warn("Auto compact failed chatId={}: {}", chatId, error.message)
		}
	}

	@Transactional
	fun compactAuto(chatId: Long): CompactResult {
		val tailKeep = AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get()
		val threshold = AppProperties.AGENT_MEMORY_COMPACT_THRESHOLD_MESSAGES.get()
		val compactable = loadCompactableMessages(chatId)
		val toCompact =
			CompactionSelection.selectForAutoCompaction(
				compactableOrdered = compactable,
				tailKeep = tailKeep,
				threshold = threshold,
			)
		return runCompaction(chatId, toCompact)
	}

	@Transactional
	fun compactManual(
		chatId: Long,
		keepRecent: Int,
	): CompactResult {
		val compactable = loadCompactableMessages(chatId)
		val toCompact =
			CompactionSelection.selectForAdminCompaction(
				compactableOrdered = compactable,
				keepRecent = keepRecent,
			)
		return runCompaction(chatId, toCompact)
	}

	private fun loadCompactableMessages(chatId: Long): List<AgentConversationMessage> =
		messageRepository.findByChatIdAndExcludedFromContextFalseAndCompactedIntoSummaryIdIsNullOrderByCreatedAtAsc(chatId)

	private fun runCompaction(
		chatId: Long,
		toCompact: List<AgentConversationMessage>,
	): CompactResult {
		if (toCompact.isEmpty()) {
			return CompactResult(compacted = false, messageCount = 0, summaryId = null)
		}

		val dialogText = formatMessagesForSummary(toCompact)
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

		val nextSequence = summaryRepository.maxSequence(chatId) + 1
		val summary =
			summaryRepository.save(
				AgentContextSummary(
					chatId = chatId,
					sequence = nextSequence,
					summaryText = summaryText,
					coversMessageIdFrom = toCompact.first().id,
					coversMessageIdTo = toCompact.last().id,
					sourceMessageCount = toCompact.size,
					model = modelName,
					tokensBefore = tokensBefore,
					tokensAfter = summaryText.length,
				),
			)
		toCompact.forEach { message ->
			message.compactedIntoSummaryId = summary.id
			messageRepository.save(message)
		}
		log.info(
			"Compacted chatId={} messages={} summaryId={} sequence={}",
			chatId,
			toCompact.size,
			summary.id,
			nextSequence,
		)
		return CompactResult(compacted = true, messageCount = toCompact.size, summaryId = summary.id)
	}

	@Transactional
	fun resetCompaction(chatId: Long): Int {
		val summaries = summaryRepository.findByChatIdOrderBySequenceAsc(chatId).size
		summaryRepository.deleteByChatId(chatId)
		messageRepository.clearCompactionMarks(chatId)
		return summaries
	}

	private fun compactionModel(): String {
		val configured = AppProperties.AGENT_MEMORY_COMPACT_MODEL.get().trim()
		return configured.ifEmpty { AppProperties.OPENROUTER_MODEL.get() }
	}

	private fun formatMessagesForSummary(messages: List<AgentConversationMessage>): String =
		buildString {
			appendLine("Диалог для сжатия (${messages.size} сообщений):")
			messages.forEach { row ->
				appendLine("--- id=${row.id} at=${row.createdAt}")
				appendLine(row.messageJson)
			}
		}

	private companion object {
		val COMPACT_SYSTEM_PROMPT =
			"""
			Сжать фрагмент диалога агента с пользователем в структурированный summary на русском.
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
