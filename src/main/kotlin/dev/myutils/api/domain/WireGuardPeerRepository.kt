package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import java.util.UUID

interface WireGuardPeerRepository : JpaRepository<WireGuardPeer, UUID> {
	fun findAllByRelayIdOrderByCreatedAtAsc(relayId: UUID): List<WireGuardPeer>

	fun findAllByRelayIdAndEnabledTrueOrderByAssignedIpAsc(relayId: UUID): List<WireGuardPeer>

	fun findByIdAndRelayId(
		id: UUID,
		relayId: UUID,
	): WireGuardPeer?

	fun findByRelayIdAndPublicKey(
		relayId: UUID,
		publicKey: String,
	): WireGuardPeer?

	fun existsByRelayIdAndNameIgnoreCase(
		relayId: UUID,
		name: String,
	): Boolean

	fun countByRelayId(relayId: UUID): Long
}
