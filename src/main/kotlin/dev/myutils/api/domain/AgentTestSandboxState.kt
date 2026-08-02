package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import jakarta.persistence.Version
import java.time.Instant

@Entity
@Table(name = "agent_test_sandbox_states")
class AgentTestSandboxState(
	@Id
	@Column(name = "memory_chat_id")
	val memoryChatId: Long,
	@Column(name = "state_json", nullable = false, columnDefinition = "TEXT")
	var stateJson: String = "{}",
	@Version
	@Column(nullable = false)
	var version: Long = 0,
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
)
