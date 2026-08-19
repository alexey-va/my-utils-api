package dev.myutils.api.web

import dev.myutils.api.service.WireGuardControlPlaneService
import dev.myutils.api.web.dto.CreateWireGuardPeerRequest
import dev.myutils.api.web.dto.CreateWireGuardRelayRequest
import dev.myutils.api.web.dto.CreatedWireGuardRelayResponse
import dev.myutils.api.web.dto.UpdateWireGuardPeerRequest
import dev.myutils.api.web.dto.WireGuardAgentTokenResponse
import dev.myutils.api.web.dto.WireGuardDesiredStateResponse
import dev.myutils.api.web.dto.WireGuardHeartbeatRequest
import dev.myutils.api.web.dto.WireGuardPeerCredentialsResponse
import dev.myutils.api.web.dto.WireGuardPeerMetricsRange
import dev.myutils.api.web.dto.WireGuardPeerMetricsResponse
import dev.myutils.api.web.dto.WireGuardPeerResponse
import dev.myutils.api.web.dto.WireGuardRelayResponse
import org.springframework.http.CacheControl
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PatchMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.util.UUID

@RestController
@RequestMapping("/api/admin/wireguard/relays")
class WireGuardAdminController(
	private val wireGuard: WireGuardControlPlaneService,
) {
	@GetMapping
	fun listRelays(): ResponseEntity<List<WireGuardRelayResponse>> =
		ResponseEntity
			.ok()
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.listRelays())

	@PostMapping
	fun createRelay(@RequestBody body: CreateWireGuardRelayRequest): ResponseEntity<CreatedWireGuardRelayResponse> =
		ResponseEntity
			.status(HttpStatus.CREATED)
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.createRelay(body))

	@PostMapping("/{relayId}/rotate-token")
	fun rotateAgentToken(@PathVariable relayId: UUID): ResponseEntity<WireGuardAgentTokenResponse> =
		ResponseEntity
			.ok()
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.rotateAgentToken(relayId))

	@DeleteMapping("/{relayId}")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun deleteRelay(@PathVariable relayId: UUID) {
		wireGuard.deleteRelay(relayId)
	}

	@GetMapping("/{relayId}/peers")
	fun listPeers(@PathVariable relayId: UUID): ResponseEntity<List<WireGuardPeerResponse>> =
		ResponseEntity
			.ok()
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.listPeers(relayId))

	@GetMapping("/{relayId}/peers/{peerId}/metrics")
	fun peerMetrics(
		@PathVariable relayId: UUID,
		@PathVariable peerId: UUID,
		@RequestParam range: WireGuardPeerMetricsRange,
	): ResponseEntity<WireGuardPeerMetricsResponse> =
		ResponseEntity
			.ok()
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.peerMetrics(relayId, peerId, range))

	@PostMapping("/{relayId}/peers")
	fun createPeer(
		@PathVariable relayId: UUID,
		@RequestBody body: CreateWireGuardPeerRequest,
	): ResponseEntity<WireGuardPeerCredentialsResponse> =
		ResponseEntity
			.status(HttpStatus.CREATED)
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.createPeer(relayId, body))

	@GetMapping("/{relayId}/peers/{peerId}/credentials")
	fun getCredentials(
		@PathVariable relayId: UUID,
		@PathVariable peerId: UUID,
	): ResponseEntity<WireGuardPeerCredentialsResponse> =
		ResponseEntity
			.ok()
			.cacheControl(CacheControl.noStore())
			.body(wireGuard.getCredentials(relayId, peerId))

	@PatchMapping("/{relayId}/peers/{peerId}")
	fun updatePeer(
		@PathVariable relayId: UUID,
		@PathVariable peerId: UUID,
		@RequestBody body: UpdateWireGuardPeerRequest,
	): WireGuardPeerResponse = wireGuard.updatePeer(relayId, peerId, body)

	@DeleteMapping("/{relayId}/peers/{peerId}")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun deletePeer(
		@PathVariable relayId: UUID,
		@PathVariable peerId: UUID,
	) {
		wireGuard.deletePeer(relayId, peerId)
	}
}

@RestController
@RequestMapping("/api/internal/wireguard/relays/{relayId}")
class WireGuardAgentController(
	private val wireGuard: WireGuardControlPlaneService,
) {
	@GetMapping("/desired")
	fun desired(@PathVariable relayId: UUID): WireGuardDesiredStateResponse = wireGuard.desiredState(relayId)

	@PostMapping("/heartbeat")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun heartbeat(
		@PathVariable relayId: UUID,
		@RequestBody body: WireGuardHeartbeatRequest,
	) {
		wireGuard.heartbeat(relayId, body)
	}
}
