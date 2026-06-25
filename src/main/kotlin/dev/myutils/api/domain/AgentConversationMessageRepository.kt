package dev.myutils.api.domain

import org.springframework.data.domain.Pageable
import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query

interface AgentConversationMessageRepository : JpaRepository<AgentConversationMessage, Long> {
	fun findByChatIdOrderByCreatedAtDesc(
		chatId: Long,
		pageable: Pageable,
	): List<AgentConversationMessage>

	fun findByChatIdAndExcludedFromContextFalseAndIsCompactedFalseOrderByCreatedAtAsc(
		chatId: Long,
	): List<AgentConversationMessage>

	fun findByChatIdAndExcludedFromContextFalseAndIsCompactedFalseOrderByCreatedAtDesc(
		chatId: Long,
		pageable: Pageable,
	): List<AgentConversationMessage>

	fun countByChatId(chatId: Long): Long

	fun countByChatIdAndExcludedFromContextFalseAndIsCompactedFalse(chatId: Long): Long

	@Query("SELECT COALESCE(MAX(m.id), 0) FROM AgentConversationMessage m WHERE m.chatId = :chatId")
	fun maxIdByChatId(chatId: Long): Long

	fun findByChatIdAndIdGreaterThanOrderByCreatedAtAsc(
		chatId: Long,
		id: Long,
	): List<AgentConversationMessage>

	fun deleteByChatId(chatId: Long)

	@Modifying
	@Query(
		"""
		UPDATE AgentConversationMessage m
		SET m.compactedIntoSummaryId = NULL
		WHERE m.compactedIntoSummaryId = :summaryId
		""",
	)
	fun detachFromSummary(summaryId: java.util.UUID)

	@Modifying
	@Query(
		"""
		UPDATE AgentConversationMessage m
		SET m.compactedIntoSummaryId = NULL, m.isCompacted = false
		WHERE m.chatId = :chatId AND m.isCompacted = true
		""",
	)
	fun clearCompactionMarks(chatId: Long)

	@Query(
		"""
		SELECT DISTINCT m.chatId FROM AgentConversationMessage m
		""",
	)
	fun findDistinctChatIds(): List<Long>

	@Query(
		"""
		SELECT m FROM AgentConversationMessage m
		WHERE m.chatId = :chatId
		  AND (:beforeId IS NULL OR m.id < :beforeId)
		ORDER BY m.createdAt DESC
		""",
	)
	fun findHistory(
		chatId: Long,
		beforeId: Long?,
		pageable: Pageable,
	): List<AgentConversationMessage>
}
