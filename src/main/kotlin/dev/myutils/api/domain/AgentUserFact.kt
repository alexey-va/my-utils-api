package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "agent_user_facts")
class AgentUserFact(
	@Id
	val id: UUID = UUID.randomUUID(),
	@Column(name = "chat_id", nullable = false)
	val chatId: Long,
	@Column(nullable = false)
	var content: String,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
	@Column(nullable = false)
	var confidence: Double = 1.0,
)
