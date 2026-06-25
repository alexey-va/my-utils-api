package dev.myutils.api.agent.memory

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import dev.myutils.api.agent.langchain.ChatMemoryMessageMapper
import dev.myutils.api.domain.AgentConversationMessage
import dev.myutils.api.domain.AgentConversationMessageRepository
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.openrouter.ChatMessage
import dev.myutils.api.properties.AppProperties
import dev.langchain4j.data.message.ChatMessage as LcChatMessage
import dev.langchain4j.store.memory.chat.ChatMemoryStore
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.data.domain.PageRequest
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional

@Service
@ConditionalOnTelegramBot
class AgentConversationStore(
	private val repository: AgentConversationMessageRepository,
	private val objectMapper: ObjectMapper,
	private val memoryAssembler: AgentMemoryAssembler,
	private val compactionService: ObjectProvider<AgentContextCompactionService>,
) : ChatMemoryStore {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun getMessages(memoryId: Any): List<LcChatMessage> = memoryAssembler.loadContextForLlm(memoryId as Long)

	override fun updateMessages(
		memoryId: Any,
		messages: List<LcChatMessage>,
	) {
		appendDelta(memoryId as Long, messages)
	}

	override fun deleteMessages(memoryId: Any) {
		repository.deleteByChatId(memoryId as Long)
	}

	fun loadRecent(
		chatId: Long,
		limit: Int = AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get(),
	): List<LcChatMessage> = memoryAssembler.loadRecentRaw(chatId, limit)

	@Transactional
	fun append(
		chatId: Long,
		messages: List<LcChatMessage>,
	) {
		val dtos = messages.mapNotNull { ChatMemoryMessageMapper.toDto(it) }
		if (dtos.isEmpty()) {
			return
		}
		persist(chatId, dtos)
		log.debug("Agent memory append chatId={} count={}", chatId, dtos.size)
		triggerAutoCompact(chatId)
	}

	@Transactional
	fun appendDelta(
		chatId: Long,
		window: List<LcChatMessage>,
	) {
		val incoming = window.mapNotNull { ChatMemoryMessageMapper.toDto(it) }
		if (incoming.isEmpty()) {
			return
		}
		val recent =
			loadRecentDtos(
				chatId,
				incoming.size.coerceAtLeast(AppProperties.AGENT_MEMORY_RECENT_MESSAGES.get()),
			)
		val toAppend = ConversationMessageDelta.findToAppend(recent, incoming)
		if (toAppend.isNotEmpty()) {
			persist(chatId, toAppend)
			log.debug("Agent memory delta chatId={} appended={}", chatId, toAppend.size)
			triggerAutoCompact(chatId)
		}
	}

	private fun triggerAutoCompact(chatId: Long) {
		compactionService.getIfAvailable()?.maybeCompactAfterAppend(chatId)
	}

	private fun loadRecentDtos(
		chatId: Long,
		limit: Int,
	): List<ChatMessage> =
		repository
			.findByChatIdAndExcludedFromContextFalseAndCompactedIntoSummaryIdIsNullOrderByCreatedAtDesc(
				chatId,
				PageRequest.of(0, limit.coerceAtLeast(1)),
			).asReversed()
			.mapNotNull { row -> decode(row.messageJson) }

	private fun persist(
		chatId: Long,
		messages: List<ChatMessage>,
	) {
		repository.saveAll(
			messages.map { dto ->
				AgentConversationMessage(
					chatId = chatId,
					messageJson = objectMapper.writeValueAsString(dto),
				)
			},
		)
	}

	private fun decode(raw: String): ChatMessage? =
		runCatching { objectMapper.readValue<ChatMessage>(raw) }.getOrNull()
}

internal object ConversationMessageDelta {
	fun findToAppend(
		existing: List<ChatMessage>,
		incoming: List<ChatMessage>,
	): List<ChatMessage> {
		for (overlap in incoming.size downTo 0) {
			if (overlap == 0) {
				return incoming
			}
			val prefix = incoming.take(overlap)
			val suffix = existing.takeLast(overlap)
			if (prefix == suffix) {
				return incoming.drop(overlap)
			}
		}
		return incoming
	}
}
