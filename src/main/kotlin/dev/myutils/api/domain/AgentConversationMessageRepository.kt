package dev.myutils.api.domain

import org.springframework.data.domain.Pageable
import org.springframework.data.jpa.repository.JpaRepository

interface AgentConversationMessageRepository : JpaRepository<AgentConversationMessage, Long> {
	fun findByChatIdOrderByCreatedAtDesc(
		chatId: Long,
		pageable: Pageable,
	): List<AgentConversationMessage>

	fun deleteByChatId(chatId: Long)
}
