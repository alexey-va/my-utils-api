package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.UUID

interface WireGuardRelayRepository : JpaRepository<WireGuardRelay, UUID> {
	fun findAllByOrderByCreatedAtAsc(): List<WireGuardRelay>

	fun existsByNameIgnoreCase(name: String): Boolean
}
