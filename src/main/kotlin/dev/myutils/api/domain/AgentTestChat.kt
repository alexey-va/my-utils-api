package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "agent_test_chats")
class AgentTestChat(
	@Id
	val id: UUID = UUID.randomUUID(),
	@Column(name = "memory_chat_id", nullable = false, unique = true)
	val memoryChatId: Long,
	@Column(nullable = false, length = 120)
	var title: String,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
)
