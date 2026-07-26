package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "agent_context_summaries")
class AgentContextSummary(
	@Id
	val id: UUID = UUID.randomUUID(),
	@Column(name = "chat_id", nullable = false)
	val chatId: Long,
	@Column(nullable = false)
	var sequence: Int,
	@Column(name = "summary_text", nullable = false, columnDefinition = "text")
	var summaryText: String,
	@Column(name = "covers_message_id_from", nullable = false)
	var coversMessageIdFrom: Long,
	@Column(name = "covers_message_id_to", nullable = false)
	var coversMessageIdTo: Long,
	@Column(name = "source_message_count", nullable = false)
	var sourceMessageCount: Int,
	@Column(length = 200)
	var model: String? = null,
	@Column(name = "tokens_before")
	var tokensBefore: Int? = null,
	@Column(name = "tokens_after")
	var tokensAfter: Int? = null,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
)
