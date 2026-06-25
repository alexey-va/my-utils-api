package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.GeneratedValue
import jakarta.persistence.GenerationType
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "agent_conversation_messages")
class AgentConversationMessage(
	@Id
	@GeneratedValue(strategy = GenerationType.IDENTITY)
	val id: Long = 0,
	@Column(name = "chat_id", nullable = false)
	val chatId: Long,
	@Column(name = "message_json", nullable = false, columnDefinition = "text")
	val messageJson: String,
	@Column(name = "excluded_from_context", nullable = false)
	var excludedFromContext: Boolean = false,
	@Column(name = "compacted_into_summary_id")
	var compactedIntoSummaryId: UUID? = null,
	@Column(name = "is_compacted", nullable = false)
	var isCompacted: Boolean = false,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
)
