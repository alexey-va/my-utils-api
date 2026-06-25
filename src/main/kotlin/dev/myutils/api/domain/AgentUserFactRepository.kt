package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.Optional
import java.util.UUID

interface AgentUserFactRepository : JpaRepository<AgentUserFact, UUID> {
	fun findByChatIdOrderByUpdatedAtDesc(chatId: Long): List<AgentUserFact>

	fun findByIdAndChatId(
		id: UUID,
		chatId: Long,
	): Optional<AgentUserFact>
}
