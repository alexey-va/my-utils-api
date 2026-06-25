package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.GeneratedValue
import jakarta.persistence.GenerationType
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant

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
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
)
