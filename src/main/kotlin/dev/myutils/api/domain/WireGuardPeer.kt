package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.FetchType
import jakarta.persistence.Id
import jakarta.persistence.JoinColumn
import jakarta.persistence.ManyToOne
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "wireguard_peers")
class WireGuardPeer(
	@Id
	val id: UUID = UUID.randomUUID(),
	@ManyToOne(fetch = FetchType.LAZY, optional = false)
	@JoinColumn(name = "relay_id", nullable = false)
	val relay: WireGuardRelay,
	@Column(nullable = false, length = 120)
	var name: String,
	@Column(name = "public_key", nullable = false, length = 64)
	val publicKey: String,
	@Column(name = "private_key_ciphertext", nullable = false, columnDefinition = "TEXT")
	val privateKeyCiphertext: String,
	@Column(name = "private_key_nonce", nullable = false, length = 64)
	val privateKeyNonce: String,
	@Column(name = "assigned_ip", nullable = false, length = 45)
	val assignedIp: String,
	@Column(nullable = false)
	var enabled: Boolean = true,
	@Column(name = "latest_handshake_at")
	var latestHandshakeAt: Instant? = null,
	@Column(name = "raw_receive_bytes", nullable = false)
	var rawReceiveBytes: Long = 0,
	@Column(name = "raw_transmit_bytes", nullable = false)
	var rawTransmitBytes: Long = 0,
	@Column(name = "total_receive_bytes", nullable = false)
	var totalReceiveBytes: Long = 0,
	@Column(name = "total_transmit_bytes", nullable = false)
	var totalTransmitBytes: Long = 0,
	@Column(name = "metrics_updated_at")
	var metricsUpdatedAt: Instant? = null,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
)
