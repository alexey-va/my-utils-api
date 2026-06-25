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

	fun findByChatIdAndExcludedFromContextFalseAndCompactedIntoSummaryIdIsNullOrderByCreatedAtAsc(
		chatId: Long,
	): List<AgentConversationMessage>

	fun findByChatIdAndExcludedFromContextFalseAndCompactedIntoSummaryIdIsNullOrderByCreatedAtDesc(
		chatId: Long,
		pageable: Pageable,
	): List<AgentConversationMessage>

	fun countByChatId(chatId: Long): Long

	fun countByChatIdAndExcludedFromContextFalseAndCompactedIntoSummaryIdIsNull(chatId: Long): Long

	fun deleteByChatId(chatId: Long)

	@Modifying
	@Query(
		"""
		UPDATE AgentConversationMessage m
		SET m.compactedIntoSummaryId = NULL
		WHERE m.chatId = :chatId AND m.compactedIntoSummaryId IS NOT NULL
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
