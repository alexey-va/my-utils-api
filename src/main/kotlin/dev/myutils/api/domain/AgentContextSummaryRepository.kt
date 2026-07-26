package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.UUID

interface AgentContextSummaryRepository : JpaRepository<AgentContextSummary, UUID> {
	fun findByChatId(chatId: Long): AgentContextSummary?

	fun findByChatIdOrderBySequenceAsc(chatId: Long): List<AgentContextSummary>

	fun deleteByChatId(chatId: Long)
}
