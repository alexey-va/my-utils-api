package dev.myutils.api.domain

import jakarta.persistence.LockModeType
import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Lock
import org.springframework.data.jpa.repository.Query
import java.util.Optional

interface AgentTestSandboxStateRepository : JpaRepository<AgentTestSandboxState, Long> {
	@Lock(LockModeType.PESSIMISTIC_WRITE)
	@Query("SELECT s FROM AgentTestSandboxState s WHERE s.memoryChatId = :memoryChatId")
	fun findForUpdate(memoryChatId: Long): Optional<AgentTestSandboxState>
}
