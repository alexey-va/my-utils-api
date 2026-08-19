package dev.myutils.api.web.dto

import java.time.Instant
import java.util.UUID

data class CreateWireGuardRelayRequest(
	val name: String,
	val publicEndpoint: String,
	val clientCidr: String,
	val clientDns: String,
)

data class WireGuardRelayResponse(
	val id: UUID,
	val name: String,
	val publicEndpoint: String,
	val clientCidr: String,
	val clientDns: String,
	val interfaceName: String,
	val serverPublicKey: String?,
	val desiredRevision: Long,
	val appliedRevision: Long?,
	val status: String,
	val lastSeenAt: Instant?,
	val routingMode: String,
	val ruPrefixCount: Int,
	val routingUpdatedAt: Instant?,
	val routeQuality: WireGuardRouteQualityResponse?,
	val createdAt: Instant,
	val updatedAt: Instant,
)

data class CreatedWireGuardRelayResponse(
	val id: UUID,
	val name: String,
	val publicEndpoint: String,
	val clientCidr: String,
	val clientDns: String,
	val interfaceName: String,
	val serverPublicKey: String?,
	val desiredRevision: Long,
	val appliedRevision: Long?,
	val status: String,
	val lastSeenAt: Instant?,
	val routingMode: String,
	val ruPrefixCount: Int,
	val routingUpdatedAt: Instant?,
	val routeQuality: WireGuardRouteQualityResponse?,
	val createdAt: Instant,
	val updatedAt: Instant,
	val agentToken: String,
)

data class WireGuardAgentTokenResponse(
	val agentToken: String,
)

data class CreateWireGuardPeerRequest(
	val name: String,
)

data class UpdateWireGuardPeerRequest(
	val enabled: Boolean,
)

data class WireGuardPeerResponse(
	val id: UUID,
	val name: String,
	val publicKey: String,
	val assignedIp: String,
	val enabled: Boolean,
	val latestHandshakeAt: Instant?,
	val totalReceiveBytes: Long,
	val totalTransmitBytes: Long,
	val metricsUpdatedAt: Instant?,
	val createdAt: Instant,
	val updatedAt: Instant,
)

data class WireGuardPeerCredentialsResponse(
	val peer: WireGuardPeerResponse,
	val clientConfig: String,
	val fileName: String,
)

data class WireGuardDesiredStateResponse(
	val revision: Long,
	val interfaceName: String,
	val peers: List<WireGuardDesiredPeerResponse>,
)

data class WireGuardDesiredPeerResponse(
	val publicKey: String,
	val allowedIp: String,
)

data class WireGuardHeartbeatRequest(
	val serverPublicKey: String,
	val publicEndpoint: String,
	val appliedRevision: Long,
	val peers: List<WireGuardPeerCounterRequest> = emptyList(),
	val routingStatus: WireGuardRoutingStatusRequest? = null,
	val routeQuality: WireGuardRouteQualityRequest? = null,
)

data class WireGuardRoutingStatusRequest(
	val mode: String,
	val ruPrefixCount: Int,
	val updatedAt: Instant,
)

data class WireGuardRouteQualityRequest(
	val measuredAt: Instant,
	val direct: WireGuardRouteProbeRequest,
	val veesp: WireGuardRouteProbeRequest,
)

data class WireGuardRouteProbeRequest(
	val target: String,
	val packetLossPercent: Double,
	val averageRttMs: Double?,
)

data class WireGuardRouteQualityResponse(
	val measuredAt: Instant,
	val direct: WireGuardRouteProbeResponse,
	val veesp: WireGuardRouteProbeResponse,
)

data class WireGuardRouteProbeResponse(
	val target: String,
	val packetLossPercent: Double,
	val averageRttMs: Double?,
)

data class WireGuardPeerCounterRequest(
	val publicKey: String,
	val latestHandshakeEpochSeconds: Long,
	val receiveBytes: Long,
	val transmitBytes: Long,
	val routingTraffic: WireGuardPeerRoutingTrafficRequest? = null,
)

data class WireGuardPeerRoutingTrafficRequest(
	val ruDownloadBytes: Long,
	val ruUploadBytes: Long,
	val nonRuDownloadBytes: Long,
	val nonRuUploadBytes: Long,
)

enum class WireGuardPeerMetricsRange {
	HOUR,
	DAY,
	WEEK,
	MONTH,
}

data class WireGuardPeerMetricPointResponse(
	val bucketStart: Instant,
	val downloadBytes: Long,
	val uploadBytes: Long,
	val ruDownloadBytes: Long,
	val ruUploadBytes: Long,
	val nonRuDownloadBytes: Long,
	val nonRuUploadBytes: Long,
)

data class WireGuardPeerMetricsResponse(
	val peerId: UUID,
	val range: WireGuardPeerMetricsRange,
	val from: Instant,
	val to: Instant,
	val points: List<WireGuardPeerMetricPointResponse>,
)
