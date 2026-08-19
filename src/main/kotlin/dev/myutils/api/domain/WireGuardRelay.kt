package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "wireguard_relays")
class WireGuardRelay(
	@Id
	val id: UUID = UUID.randomUUID(),
	@Column(nullable = false, unique = true, length = 80)
	var name: String,
	@Column(name = "public_endpoint", nullable = false, length = 255)
	var publicEndpoint: String,
	@Column(name = "client_cidr", nullable = false, length = 32)
	val clientCidr: String,
	@Column(name = "client_dns", nullable = false, length = 64)
	var clientDns: String,
	@Column(name = "interface_name", nullable = false, length = 15)
	val interfaceName: String = "wg-users",
	@Column(name = "agent_token_hash", nullable = false, length = 64)
	var agentTokenHash: String,
	@Column(name = "server_public_key", length = 64)
	var serverPublicKey: String? = null,
	@Column(name = "desired_revision", nullable = false)
	var desiredRevision: Long = 0,
	@Column(name = "applied_revision")
	var appliedRevision: Long? = null,
	@Column(name = "last_seen_at")
	var lastSeenAt: Instant? = null,
	@Column(name = "routing_mode", nullable = false, length = 32)
	var routingMode: String = "AWG_ONLY",
	@Column(name = "ru_prefix_count", nullable = false)
	var ruPrefixCount: Int = 0,
	@Column(name = "routing_updated_at")
	var routingUpdatedAt: Instant? = null,
	@Column(name = "created_at", nullable = false)
	val createdAt: Instant = Instant.now(),
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
)
