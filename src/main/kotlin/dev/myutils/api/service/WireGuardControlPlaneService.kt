package dev.myutils.api.service

import dev.myutils.api.domain.WireGuardPeer
import dev.myutils.api.domain.WireGuardPeerMetricSample
import dev.myutils.api.domain.WireGuardPeerMetricSampleRepository
import dev.myutils.api.domain.WireGuardPeerRepository
import dev.myutils.api.domain.WireGuardRelay
import dev.myutils.api.domain.WireGuardRelayRepository
import dev.myutils.api.web.dto.CreateWireGuardPeerRequest
import dev.myutils.api.web.dto.CreateWireGuardRelayRequest
import dev.myutils.api.web.dto.CreatedWireGuardRelayResponse
import dev.myutils.api.web.dto.UpdateWireGuardPeerRequest
import dev.myutils.api.web.dto.WireGuardAgentTokenResponse
import dev.myutils.api.web.dto.WireGuardDesiredPeerResponse
import dev.myutils.api.web.dto.WireGuardDesiredStateResponse
import dev.myutils.api.web.dto.WireGuardHeartbeatRequest
import dev.myutils.api.web.dto.WireGuardPeerMetricPointResponse
import dev.myutils.api.web.dto.WireGuardPeerMetricsRange
import dev.myutils.api.web.dto.WireGuardPeerMetricsResponse
import dev.myutils.api.web.dto.WireGuardPeerCredentialsResponse
import dev.myutils.api.web.dto.WireGuardPeerResponse
import dev.myutils.api.web.dto.WireGuardRouteProbeRequest
import dev.myutils.api.web.dto.WireGuardRouteProbeResponse
import dev.myutils.api.web.dto.WireGuardRouteQualityResponse
import dev.myutils.api.web.dto.WireGuardRelayResponse
import dev.myutils.api.wireguard.EncryptedSecret
import dev.myutils.api.wireguard.Ipv4Cidr
import dev.myutils.api.wireguard.WireGuardClientConfig
import dev.myutils.api.wireguard.WireGuardCredentialsCipher
import dev.myutils.api.wireguard.WireGuardKeyPairGenerator
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.util.Base64
import java.util.UUID

@Service
class WireGuardControlPlaneService(
	private val relays: WireGuardRelayRepository,
	private val peers: WireGuardPeerRepository,
	private val metricSamples: WireGuardPeerMetricSampleRepository,
	private val keyPairGenerator: WireGuardKeyPairGenerator,
	private val credentialsCipher: WireGuardCredentialsCipher,
	private val clock: Clock = Clock.systemUTC(),
	private val secureRandom: SecureRandom = SecureRandom(),
) {
	@Transactional(readOnly = true)
	fun listRelays(): List<WireGuardRelayResponse> = relays.findAllByOrderByCreatedAtAsc().map(::relayResponse)

	@Transactional
	fun createRelay(body: CreateWireGuardRelayRequest): CreatedWireGuardRelayResponse {
		val name = requiredText(body.name, "Relay name", 80)
		if (relays.existsByNameIgnoreCase(name)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Relay name already exists")
		}
		val endpoint = validateEndpoint(body.publicEndpoint)
		val cidr = parseCidr(body.clientCidr)
		val dns = validateIpv4(body.clientDns, "Client DNS")
		val token = generateToken()
		val relay =
			relays.save(
				WireGuardRelay(
					name = name,
					publicEndpoint = endpoint,
					clientCidr = cidr.value,
					clientDns = dns,
					agentTokenHash = tokenHash(token),
				),
			)
		return relayResponse(relay).withAgentToken(token)
	}

	@Transactional
	fun rotateAgentToken(relayId: UUID): WireGuardAgentTokenResponse {
		val relay = relay(relayId)
		val token = generateToken()
		relay.agentTokenHash = tokenHash(token)
		relay.updatedAt = now()
		return WireGuardAgentTokenResponse(token)
	}

	@Transactional
	fun deleteRelay(relayId: UUID) {
		val relay = relay(relayId)
		if (peers.countByRelayId(relayId) > 0) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Delete relay peers first")
		}
		relays.delete(relay)
	}

	@Transactional(readOnly = true)
	fun listPeers(relayId: UUID): List<WireGuardPeerResponse> {
		relay(relayId)
		return peers.findAllByRelayIdOrderByCreatedAtAsc(relayId).map(::peerResponse)
	}

	@Transactional(readOnly = true)
	fun peerMetrics(
		relayId: UUID,
		peerId: UUID,
		range: WireGuardPeerMetricsRange,
	): WireGuardPeerMetricsResponse {
		relay(relayId)
		peer(relayId, peerId)
		val to = now()
		val (window, bucket) = metricWindow(range)
		val from = to.minus(window)
		val points =
			metricSamples
				.findAllByPeerIdAndRecordedAtGreaterThanEqualAndRecordedAtLessThanOrderByRecordedAtAsc(
					peerId,
					from,
					to,
				).groupBy { sample -> Duration.between(from, sample.recordedAt).toMillis() / bucket.toMillis() }
				.toSortedMap()
				.map { (bucketIndex, bucketSamples) ->
					WireGuardPeerMetricPointResponse(
						bucketStart = from.plusMillis(bucketIndex * bucket.toMillis()),
						downloadBytes = bucketSamples.sumOf(WireGuardPeerMetricSample::downloadBytes),
						uploadBytes = bucketSamples.sumOf(WireGuardPeerMetricSample::uploadBytes),
						ruDownloadBytes = bucketSamples.sumOf(WireGuardPeerMetricSample::ruDownloadBytes),
						ruUploadBytes = bucketSamples.sumOf(WireGuardPeerMetricSample::ruUploadBytes),
						nonRuDownloadBytes = bucketSamples.sumOf(WireGuardPeerMetricSample::nonRuDownloadBytes),
						nonRuUploadBytes = bucketSamples.sumOf(WireGuardPeerMetricSample::nonRuUploadBytes),
					)
				}
		return WireGuardPeerMetricsResponse(
			peerId = peerId,
			range = range,
			from = from,
			to = to,
			points = points,
		)
	}

	@Transactional
	fun createPeer(
		relayId: UUID,
		body: CreateWireGuardPeerRequest,
	): WireGuardPeerCredentialsResponse {
		val relay = relay(relayId)
		val serverPublicKey =
			relay.serverPublicKey
				?: throw ResponseStatusException(HttpStatus.CONFLICT, "Relay has not reported its server public key")
		if (!credentialsCipher.isConfigured) {
			throw ResponseStatusException(HttpStatus.SERVICE_UNAVAILABLE, "WireGuard credential encryption is not configured")
		}
		val name = requiredText(body.name, "Peer name", 120)
		if (peers.existsByRelayIdAndNameIgnoreCase(relayId, name)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Peer name already exists")
		}
		val cidr = parseCidr(relay.clientCidr)
		val used = peers.findAllByRelayIdOrderByCreatedAtAsc(relayId).mapTo(HashSet()) { it.assignedIp }
		val assignedIp =
			(2..cidr.lastUsableHostOffset)
				.asSequence()
				.map(cidr::hostAddress)
				.firstOrNull { it !in used }
				?: throw ResponseStatusException(HttpStatus.CONFLICT, "Relay client CIDR is exhausted")
		val pair = keyPairGenerator.generate()
		val encrypted = credentialsCipher.encrypt(pair.privateKey)
		val peer =
			peers.save(
				WireGuardPeer(
					relay = relay,
					name = name,
					publicKey = pair.publicKey,
					privateKeyCiphertext = encrypted.ciphertext,
					privateKeyNonce = encrypted.nonce,
					assignedIp = assignedIp,
				),
			)
		relay.desiredRevision += 1
		relay.updatedAt = now()
		return credentialsResponse(peer, relay, pair.privateKey, serverPublicKey)
	}

	@Transactional(readOnly = true)
	fun getCredentials(
		relayId: UUID,
		peerId: UUID,
	): WireGuardPeerCredentialsResponse {
		val relay = relay(relayId)
		val peer = peer(relayId, peerId)
		val serverPublicKey =
			relay.serverPublicKey
				?: throw ResponseStatusException(HttpStatus.CONFLICT, "Relay has not reported its server public key")
		if (!credentialsCipher.isConfigured) {
			throw ResponseStatusException(HttpStatus.SERVICE_UNAVAILABLE, "WireGuard credential encryption is not configured")
		}
		val privateKey =
			credentialsCipher.decrypt(
				EncryptedSecret(
					ciphertext = peer.privateKeyCiphertext,
					nonce = peer.privateKeyNonce,
				),
			)
		return credentialsResponse(peer, relay, privateKey, serverPublicKey)
	}

	@Transactional
	fun updatePeer(
		relayId: UUID,
		peerId: UUID,
		body: UpdateWireGuardPeerRequest,
	): WireGuardPeerResponse {
		val relay = relay(relayId)
		val peer = peer(relayId, peerId)
		if (peer.enabled != body.enabled) {
			peer.enabled = body.enabled
			peer.updatedAt = now()
			relay.desiredRevision += 1
			relay.updatedAt = now()
		}
		return peerResponse(peer)
	}

	@Transactional
	fun deletePeer(
		relayId: UUID,
		peerId: UUID,
	) {
		val relay = relay(relayId)
		val peer = peer(relayId, peerId)
		peers.delete(peer)
		relay.desiredRevision += 1
		relay.updatedAt = now()
	}

	@Transactional(readOnly = true)
	fun desiredState(relayId: UUID): WireGuardDesiredStateResponse {
		val relay = relay(relayId)
		return WireGuardDesiredStateResponse(
			revision = relay.desiredRevision,
			interfaceName = relay.interfaceName,
			peers =
				peers.findAllByRelayIdAndEnabledTrueOrderByAssignedIpAsc(relayId).map {
					WireGuardDesiredPeerResponse(publicKey = it.publicKey, allowedIp = "${it.assignedIp}/32")
				},
		)
	}

	@Transactional
	fun heartbeat(
		relayId: UUID,
		body: WireGuardHeartbeatRequest,
	) {
		val relay = relay(relayId)
		val serverPublicKey = validateWireGuardPublicKey(body.serverPublicKey)
		val endpoint = validateEndpoint(body.publicEndpoint)
		if (body.appliedRevision < 0 || body.appliedRevision > relay.desiredRevision) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Applied revision is invalid")
		}
		body.peers.forEach { counter ->
			validateWireGuardPublicKey(counter.publicKey)
			if (
				counter.latestHandshakeEpochSeconds < 0 ||
				counter.receiveBytes < 0 ||
				counter.transmitBytes < 0
			) {
				throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Peer counters must not be negative")
			}
			counter.routingTraffic?.let { traffic ->
				if (
					traffic.ruDownloadBytes < 0 ||
					traffic.ruUploadBytes < 0 ||
					traffic.nonRuDownloadBytes < 0 ||
					traffic.nonRuUploadBytes < 0
				) {
					throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Peer routing counters must not be negative")
				}
			}
		}

		val timestamp = now()
		relay.serverPublicKey = serverPublicKey
		relay.publicEndpoint = endpoint
		relay.appliedRevision = body.appliedRevision
		relay.lastSeenAt = timestamp
		relay.updatedAt = timestamp
		body.routingStatus?.let { status ->
			if (status.mode !in ROUTING_MODES || status.ruPrefixCount !in 0..MAX_RU_PREFIX_COUNT) {
				throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Routing status is invalid")
			}
			if (
				(status.mode == ROUTING_MODE_AWG_ONLY && status.ruPrefixCount != 0) ||
				(status.mode == ROUTING_MODE_RU_DIRECT && status.ruPrefixCount == 0) ||
				status.updatedAt.isAfter(timestamp.plus(ROUTING_CLOCK_SKEW))
			) {
				throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Routing status is inconsistent")
			}
			relay.routingMode = status.mode
			relay.ruPrefixCount = status.ruPrefixCount
			relay.routingUpdatedAt = status.updatedAt
		}
		body.routeQuality?.let { quality ->
			if (quality.measuredAt.isAfter(timestamp.plus(ROUTING_CLOCK_SKEW))) {
				throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Route quality timestamp is invalid")
			}
			val direct = validateRouteProbe(quality.direct)
			val veesp = validateRouteProbe(quality.veesp)
			relay.directProbeTarget = direct.target
			relay.directPacketLossPercent = direct.packetLossPercent
			relay.directAverageRttMs = direct.averageRttMs
			relay.veespProbeTarget = veesp.target
			relay.veespPacketLossPercent = veesp.packetLossPercent
			relay.veespAverageRttMs = veesp.averageRttMs
			relay.routeQualityUpdatedAt = quality.measuredAt
		}
		body.peers.forEach { counter ->
			val peer = peers.findByRelayIdAndPublicKey(relayId, counter.publicKey) ?: return@forEach
			val receiveDelta = counterDelta(peer.rawReceiveBytes, counter.receiveBytes)
			val transmitDelta = counterDelta(peer.rawTransmitBytes, counter.transmitBytes)
			peer.totalReceiveBytes += receiveDelta
			peer.totalTransmitBytes += transmitDelta
			peer.rawReceiveBytes = counter.receiveBytes
			peer.rawTransmitBytes = counter.transmitBytes
			if (counter.latestHandshakeEpochSeconds > 0) {
				peer.latestHandshakeAt = Instant.ofEpochSecond(counter.latestHandshakeEpochSeconds)
			}
			peer.metricsUpdatedAt = timestamp
			peer.updatedAt = timestamp
			val routingTraffic = counter.routingTraffic
			metricSamples.save(
				WireGuardPeerMetricSample(
					peer = peer,
					recordedAt = timestamp,
					downloadBytes = transmitDelta,
					uploadBytes = receiveDelta,
					ruDownloadBytes = routingTraffic?.ruDownloadBytes ?: 0,
					ruUploadBytes = routingTraffic?.ruUploadBytes ?: 0,
					nonRuDownloadBytes = routingTraffic?.nonRuDownloadBytes ?: 0,
					nonRuUploadBytes = routingTraffic?.nonRuUploadBytes ?: 0,
					latestHandshakeAt = peer.latestHandshakeAt,
				),
			)
		}
		metricSamples.deleteRecordedBefore(timestamp.minus(METRIC_RETENTION))
	}

	@Transactional(readOnly = true)
	fun agentTokenMatches(
		relayId: UUID,
		token: String,
	): Boolean {
		if (token.length !in 40..512) return false
		val expected = relays.findById(relayId).orElse(null)?.agentTokenHash ?: return false
		return MessageDigest.isEqual(
			expected.toByteArray(Charsets.US_ASCII),
			tokenHash(token).toByteArray(Charsets.US_ASCII),
		)
	}

	private fun relayResponse(relay: WireGuardRelay): WireGuardRelayResponse =
		WireGuardRelayResponse(
			id = relay.id,
			name = relay.name,
			publicEndpoint = relay.publicEndpoint,
			clientCidr = relay.clientCidr,
			clientDns = relay.clientDns,
			interfaceName = relay.interfaceName,
			serverPublicKey = relay.serverPublicKey,
			desiredRevision = relay.desiredRevision,
			appliedRevision = relay.appliedRevision,
			status = relayStatus(relay),
			lastSeenAt = relay.lastSeenAt,
			routingMode = relay.routingMode,
			ruPrefixCount = relay.ruPrefixCount,
			routingUpdatedAt = relay.routingUpdatedAt,
			routeQuality = routeQualityResponse(relay),
			createdAt = relay.createdAt,
			updatedAt = relay.updatedAt,
		)

	private fun WireGuardRelayResponse.withAgentToken(token: String): CreatedWireGuardRelayResponse =
		CreatedWireGuardRelayResponse(
			id = id,
			name = name,
			publicEndpoint = publicEndpoint,
			clientCidr = clientCidr,
			clientDns = clientDns,
			interfaceName = interfaceName,
			serverPublicKey = serverPublicKey,
			desiredRevision = desiredRevision,
			appliedRevision = appliedRevision,
			status = status,
			lastSeenAt = lastSeenAt,
			routingMode = routingMode,
			ruPrefixCount = ruPrefixCount,
			routingUpdatedAt = routingUpdatedAt,
			routeQuality = routeQuality,
			createdAt = createdAt,
			updatedAt = updatedAt,
			agentToken = token,
		)

	private fun peerResponse(peer: WireGuardPeer): WireGuardPeerResponse =
		WireGuardPeerResponse(
			id = peer.id,
			name = peer.name,
			publicKey = peer.publicKey,
			assignedIp = peer.assignedIp,
			enabled = peer.enabled,
			latestHandshakeAt = peer.latestHandshakeAt,
			totalReceiveBytes = peer.totalReceiveBytes,
			totalTransmitBytes = peer.totalTransmitBytes,
			metricsUpdatedAt = peer.metricsUpdatedAt,
			createdAt = peer.createdAt,
			updatedAt = peer.updatedAt,
		)

	private fun credentialsResponse(
		peer: WireGuardPeer,
		relay: WireGuardRelay,
		privateKey: String,
		serverPublicKey: String,
	): WireGuardPeerCredentialsResponse =
		WireGuardPeerCredentialsResponse(
			peer = peerResponse(peer),
			clientConfig =
				WireGuardClientConfig.render(
					privateKey = privateKey,
					address = peer.assignedIp,
					dns = relay.clientDns,
					serverPublicKey = serverPublicKey,
					endpoint = relay.publicEndpoint,
				),
			fileName = "${fileSlug(peer.name, peer.id)}.conf",
		)

	private fun relayStatus(relay: WireGuardRelay): String =
		when {
			relay.serverPublicKey == null || relay.lastSeenAt == null -> "WAITING_FOR_AGENT"
			Duration.between(relay.lastSeenAt, now()) > STALE_AFTER -> "STALE"
			relay.appliedRevision == relay.desiredRevision -> "READY"
			else -> "SYNCING"
		}

	private fun relay(relayId: UUID): WireGuardRelay =
		relays.findById(relayId).orElseThrow {
			ResponseStatusException(HttpStatus.NOT_FOUND, "WireGuard relay not found")
		}

	private fun peer(
		relayId: UUID,
		peerId: UUID,
	): WireGuardPeer =
		peers.findByIdAndRelayId(peerId, relayId)
			?: throw ResponseStatusException(HttpStatus.NOT_FOUND, "WireGuard peer not found")

	private fun requiredText(
		value: String,
		label: String,
		maxLength: Int,
	): String {
		val normalized = value.trim()
		if (normalized.isEmpty() || normalized.length > maxLength || '\n' in normalized || '\r' in normalized) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "$label is invalid")
		}
		return normalized
	}

	private fun validateEndpoint(value: String): String {
		val normalized = requiredText(value, "Public endpoint", 255)
		val separator = normalized.lastIndexOf(':')
		val host = normalized.substring(0, separator.coerceAtLeast(0))
		val port = normalized.substring((separator + 1).coerceAtMost(normalized.length)).toIntOrNull()
		if (separator <= 0 || host.any(Char::isWhitespace) || port == null || port !in 1..65535) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Public endpoint must be host:port")
		}
		return normalized
	}

	private fun validateIpv4(
		value: String,
		label: String,
	): String {
		val normalized = value.trim()
		val parts = normalized.split('.')
		if (
			parts.size != 4 ||
			parts.any { part -> part.toIntOrNull()?.let { it in 0..255 } != true }
		) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "$label must be an IPv4 address")
		}
		return normalized
	}

	private fun parseCidr(value: String): Ipv4Cidr =
		try {
			Ipv4Cidr.parse(value)
		} catch (ex: IllegalArgumentException) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, ex.message ?: "Invalid client CIDR")
		}

	private fun validateWireGuardPublicKey(value: String): String {
		val decoded = runCatching { Base64.getDecoder().decode(value) }.getOrNull()
		if (decoded?.size != 32) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "WireGuard public key is invalid")
		}
		return value
	}

	private fun validateRouteProbe(probe: WireGuardRouteProbeRequest): WireGuardRouteProbeRequest {
		val target = validateIpv4(probe.target, "Route probe target")
		val loss = probe.packetLossPercent
		val rtt = probe.averageRttMs
		if (
			!loss.isFinite() || loss !in 0.0..100.0 ||
			(rtt != null && (!rtt.isFinite() || rtt < 0 || rtt > MAX_PROBE_RTT_MS)) ||
			(loss < 100.0 && rtt == null)
		) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Route probe values are invalid")
		}
		return probe.copy(target = target)
	}

	private fun routeQualityResponse(relay: WireGuardRelay): WireGuardRouteQualityResponse? {
		val measuredAt = relay.routeQualityUpdatedAt ?: return null
		val directTarget = relay.directProbeTarget ?: return null
		val directLoss = relay.directPacketLossPercent ?: return null
		val veespTarget = relay.veespProbeTarget ?: return null
		val veespLoss = relay.veespPacketLossPercent ?: return null
		return WireGuardRouteQualityResponse(
			measuredAt = measuredAt,
			direct = WireGuardRouteProbeResponse(directTarget, directLoss, relay.directAverageRttMs),
			veesp = WireGuardRouteProbeResponse(veespTarget, veespLoss, relay.veespAverageRttMs),
		)
	}

	private fun generateToken(): String =
		ByteArray(TOKEN_BYTES)
			.also(secureRandom::nextBytes)
			.let(Base64.getUrlEncoder().withoutPadding()::encodeToString)

	private fun tokenHash(token: String): String =
		MessageDigest
			.getInstance("SHA-256")
			.digest(token.toByteArray(Charsets.UTF_8))
			.joinToString("") { "%02x".format(it) }

	private fun fileSlug(
		name: String,
		id: UUID,
	): String =
		name
			.lowercase()
			.replace(Regex("[^a-z0-9]+"), "-")
			.trim('-')
			.take(80)
			.ifEmpty { "wireguard-peer-${id.toString().take(8)}" }

	private fun counterDelta(
		previous: Long,
		current: Long,
	): Long = if (current >= previous) current - previous else current

	private fun metricWindow(range: WireGuardPeerMetricsRange): Pair<Duration, Duration> =
		when (range) {
			WireGuardPeerMetricsRange.HOUR -> Duration.ofHours(1) to Duration.ofMinutes(1)
			WireGuardPeerMetricsRange.DAY -> Duration.ofDays(1) to Duration.ofMinutes(15)
			WireGuardPeerMetricsRange.WEEK -> Duration.ofDays(7) to Duration.ofHours(1)
			WireGuardPeerMetricsRange.MONTH -> Duration.ofDays(30) to Duration.ofHours(6)
		}

	private fun now(): Instant = clock.instant()

	private companion object {
		const val TOKEN_BYTES = 32
		const val MAX_RU_PREFIX_COUNT = 100_000
		const val MAX_PROBE_RTT_MS = 60_000.0
		const val ROUTING_MODE_AWG_ONLY = "AWG_ONLY"
		const val ROUTING_MODE_RU_DIRECT = "RU_DIRECT_AWG_DEFAULT"
		val ROUTING_MODES = setOf(ROUTING_MODE_AWG_ONLY, ROUTING_MODE_RU_DIRECT)
		val ROUTING_CLOCK_SKEW: Duration = Duration.ofMinutes(5)
		val METRIC_RETENTION: Duration = Duration.ofDays(31)
		val STALE_AFTER: Duration = Duration.ofMinutes(2)
	}
}
