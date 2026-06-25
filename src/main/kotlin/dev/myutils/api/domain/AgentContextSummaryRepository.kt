package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query
import java.util.UUID

interface AgentContextSummaryRepository : JpaRepository<AgentContextSummary, UUID> {
	fun findByChatIdOrderBySequenceAsc(chatId: Long): List<AgentContextSummary>

	fun deleteByChatId(chatId: Long)

	@Query("SELECT COALESCE(MAX(s.sequence), 0) FROM AgentContextSummary s WHERE s.chatId = :chatId")
	fun maxSequence(chatId: Long): Int
}
