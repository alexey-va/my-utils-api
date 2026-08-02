package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Query
import java.util.UUID

interface AgentTestChatRepository : JpaRepository<AgentTestChat, UUID> {
	fun findAllByOrderByUpdatedAtDesc(): List<AgentTestChat>

	@Query(value = "SELECT nextval('agent_test_chat_memory_id_seq')", nativeQuery = true)
	fun nextMemoryChatId(): Long
}
