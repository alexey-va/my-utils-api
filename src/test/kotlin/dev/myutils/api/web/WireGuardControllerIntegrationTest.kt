package dev.myutils.api.web

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.domain.WireGuardPeerRepository
import dev.myutils.api.domain.WireGuardRelayRepository
import dev.myutils.api.testkit.TestingIntegrationTestBase
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.security.crypto.password.PasswordEncoder
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.delete
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.patch
import org.springframework.test.web.servlet.post
import java.time.Instant
import java.util.Base64
import java.util.UUID

@AutoConfigureMockMvc
class WireGuardControllerIntegrationTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Autowired
	private lateinit var users: UserRepository

	@Autowired
	private lateinit var passwordEncoder: PasswordEncoder

	@Autowired
	private lateinit var relays: WireGuardRelayRepository

	@Autowired
	private lateinit var peers: WireGuardPeerRepository

	private lateinit var adminToken: String
	private lateinit var userToken: String
	private val createdUserIds = mutableListOf<UUID>()
	private val createdRelayIds = mutableListOf<UUID>()
	private val serverPublicKey = Base64.getEncoder().encodeToString(ByteArray(32) { 7 })

	@BeforeEach
	fun createAccounts() {
		adminToken = createAndLogin(UserRole.ADMIN)
		userToken = createAndLogin(UserRole.USER)
	}

	@AfterEach
	fun cleanup() {
		createdRelayIds.forEach { relayId ->
			peers.deleteAll(peers.findAllByRelayIdOrderByCreatedAtAsc(relayId))
			relays.findById(relayId).ifPresent(relays::delete)
		}
		createdUserIds.forEach { userId -> users.findById(userId).ifPresent(users::delete) }
		createdRelayIds.clear()
		createdUserIds.clear()
	}

	@Test
	fun `wireguard administration requires a real administrator`() {
		mockMvc.get("/api/admin/wireguard/relays").andExpect {
			status { isUnauthorized() }
		}

		mockMvc
			.get("/api/admin/wireguard/relays") {
				bearer(userToken)
			}.andExpect {
				status { isForbidden() }
			}
	}

	@Test
	fun `admin provisions relay and peer then retrieves the same credentials again`() {
		val relay = createRelay()
		val relayId = UUID.fromString(relay["id"].asText())
		val agentToken = relay["agentToken"].asText()

		assertEquals("WAITING_FOR_AGENT", relay["status"].asText())
		assertTrue(agentToken.length >= 40)
		assertFalse(relays.findById(relayId).orElseThrow().agentTokenHash.contains(agentToken))

		mockMvc
			.post("/api/admin/wireguard/relays/$relayId/peers") {
				admin()
				contentType = MediaType.APPLICATION_JSON
				content = """{"name":"Alex phone"}"""
			}.andExpect {
				status { isConflict() }
			}

		heartbeat(relayId, agentToken, appliedRevision = 0, counters = emptyList())

		val createdPeer =
			mockMvc
				.post("/api/admin/wireguard/relays/$relayId/peers") {
					admin()
					contentType = MediaType.APPLICATION_JSON
					content = """{"name":"Alex phone"}"""
				}.andExpect {
					status { isCreated() }
					jsonPath("$.peer.name") { value("Alex phone") }
					jsonPath("$.peer.assignedIp") { value("10.89.0.2") }
					jsonPath("$.fileName") { value("alex-phone.conf") }
					jsonPath("$.clientConfig") { exists() }
				}.andReturn()
		val createdBody = objectMapper.readTree(createdPeer.response.contentAsString)
		val peerId = UUID.fromString(createdBody["peer"]["id"].asText())
		val firstConfig = createdBody["clientConfig"].asText()
		assertTrue(firstConfig.contains("Endpoint = 203.0.113.10:51820"))
		assertTrue(firstConfig.contains("PublicKey = $serverPublicKey"))

		val storedPeer = peers.findById(peerId).orElseThrow()
		assertFalse(firstConfig.contains(storedPeer.privateKeyCiphertext))
		assertFalse(storedPeer.privateKeyCiphertext.contains("PrivateKey ="))

		mockMvc
			.get("/api/admin/wireguard/relays/$relayId/peers/$peerId/credentials") {
				admin()
			}.andExpect {
				status { isOk() }
				header { string("Cache-Control", "no-store") }
				jsonPath("$.clientConfig") { value(firstConfig) }
				jsonPath("$.fileName") { value("alex-phone.conf") }
			}

		mockMvc
			.get("/api/admin/wireguard/relays/$relayId/peers") {
				admin()
			}.andExpect {
				status { isOk() }
				jsonPath("$[0].id") { value(peerId.toString()) }
				jsonPath("$[0].clientConfig") { doesNotExist() }
				jsonPath("$[0].privateKey") { doesNotExist() }
			}

		mockMvc
			.get("/api/internal/wireguard/relays/$relayId/desired") {
				agent(agentToken)
			}.andExpect {
				status { isOk() }
				jsonPath("$.revision") { value(1) }
				jsonPath("$.interfaceName") { value("wg-users") }
				jsonPath("$.peers[0].publicKey") { value(storedPeer.publicKey) }
				jsonPath("$.peers[0].allowedIp") { value("10.89.0.2/32") }
			}
	}

	@Test
	fun `agent authentication fails closed and heartbeat accumulates reset counters`() {
		val relay = createRelay()
		val relayId = UUID.fromString(relay["id"].asText())
		val agentToken = relay["agentToken"].asText()

		mockMvc.get("/api/internal/wireguard/relays/$relayId/desired").andExpect {
			status { isUnauthorized() }
		}
		mockMvc
			.get("/api/internal/wireguard/relays/$relayId/desired") {
				agent("wrong-token")
			}.andExpect {
				status { isUnauthorized() }
			}

		heartbeat(relayId, agentToken, appliedRevision = 0, counters = emptyList())
		val peerBody = createPeer(relayId, "Traffic peer")
		val peerId = UUID.fromString(peerBody["peer"]["id"].asText())
		val publicKey = peerBody["peer"]["publicKey"].asText()
		val handshake = Instant.parse("2026-08-19T10:00:00Z")

		heartbeat(relayId, agentToken, 1, listOf(Counter(publicKey, handshake.epochSecond, 100, 200)))
		heartbeat(relayId, agentToken, 1, listOf(Counter(publicKey, handshake.epochSecond, 150, 260)))
		heartbeat(relayId, agentToken, 1, listOf(Counter(publicKey, handshake.epochSecond, 10, 20)))

		mockMvc
			.get("/api/admin/wireguard/relays/$relayId/peers") {
				admin()
			}.andExpect {
				status { isOk() }
				jsonPath("$[0].id") { value(peerId.toString()) }
				jsonPath("$[0].latestHandshakeAt") { value("2026-08-19T10:00:00Z") }
				jsonPath("$[0].totalReceiveBytes") { value(160) }
				jsonPath("$[0].totalTransmitBytes") { value(280) }
			}

		mockMvc
			.patch("/api/admin/wireguard/relays/$relayId/peers/$peerId") {
				admin()
				contentType = MediaType.APPLICATION_JSON
				content = """{"enabled":false}"""
			}.andExpect {
				status { isOk() }
				jsonPath("$.enabled") { value(false) }
			}

		mockMvc
			.get("/api/internal/wireguard/relays/$relayId/desired") {
				agent(agentToken)
			}.andExpect {
				status { isOk() }
				jsonPath("$.revision") { value(2) }
				jsonPath("$.peers.length()") { value(0) }
			}

		mockMvc
			.delete("/api/admin/wireguard/relays/$relayId/peers/$peerId") {
				admin()
			}.andExpect {
				status { isNoContent() }
			}
	}

	@Test
	fun `heartbeat exposes routing state and peer traffic history with client directions`() {
		val relay = createRelay()
		val relayId = UUID.fromString(relay["id"].asText())
		val agentToken = relay["agentToken"].asText()

		heartbeat(relayId, agentToken, appliedRevision = 0, counters = emptyList())
		val peerBody = createPeer(relayId, "Chart peer")
		val peerId = UUID.fromString(peerBody["peer"]["id"].asText())
		val publicKey = peerBody["peer"]["publicKey"].asText()
		val handshake = Instant.parse("2026-08-19T18:05:11Z")

		heartbeat(
			relayId,
			agentToken,
			appliedRevision = 1,
			counters =
				listOf(
					Counter(
						publicKey,
						handshake.epochSecond,
						receiveBytes = 100,
						transmitBytes = 200,
						routingTraffic = RoutingTraffic(80, 40, 120, 60),
					),
				),
			routingStatus =
				mapOf(
					"mode" to "RU_DIRECT_AWG_DEFAULT",
					"ruPrefixCount" to 8_642,
					"updatedAt" to "2026-08-19T18:00:00Z",
				),
		)
		heartbeat(
			relayId,
			agentToken,
			appliedRevision = 1,
			counters =
				listOf(
					Counter(
						publicKey,
						handshake.epochSecond,
						receiveBytes = 150,
						transmitBytes = 260,
						routingTraffic = RoutingTraffic(20, 15, 40, 35),
					),
				),
			routingStatus =
				mapOf(
					"mode" to "RU_DIRECT_AWG_DEFAULT",
					"ruPrefixCount" to 8_642,
					"updatedAt" to "2026-08-19T18:00:00Z",
				),
			routeQuality =
				mapOf(
					"measuredAt" to "2026-08-19T18:00:00Z",
					"direct" to
						mapOf(
							"target" to "77.88.8.8",
							"packetLossPercent" to 0.0,
							"averageRttMs" to 2.7,
						),
					"veesp" to
						mapOf(
							"target" to "185.242.106.81",
							"packetLossPercent" to 20.0,
							"averageRttMs" to 26.6,
						),
				),
		)

		mockMvc
			.get("/api/admin/wireguard/relays") {
				admin()
			}.andExpect {
				status { isOk() }
				header { string("Cache-Control", "no-store") }
				jsonPath("$[0].routingMode") { value("RU_DIRECT_AWG_DEFAULT") }
				jsonPath("$[0].ruPrefixCount") { value(8_642) }
				jsonPath("$[0].routingUpdatedAt") { value("2026-08-19T18:00:00Z") }
				jsonPath("$[0].routeQuality.direct.packetLossPercent") { value(0.0) }
				jsonPath("$[0].routeQuality.direct.averageRttMs") { value(2.7) }
				jsonPath("$[0].routeQuality.veesp.packetLossPercent") { value(20.0) }
				jsonPath("$[0].routeQuality.veesp.averageRttMs") { value(26.6) }
				jsonPath("$[0].routeQuality.measuredAt") { value("2026-08-19T18:00:00Z") }
			}

		mockMvc
			.get("/api/admin/wireguard/relays/$relayId/peers") {
				admin()
			}.andExpect {
				status { isOk() }
				header { string("Cache-Control", "no-store") }
			}

		val metricsResponse =
			mockMvc
				.get("/api/admin/wireguard/relays/$relayId/peers/$peerId/metrics") {
					admin()
					param("range", "HOUR")
				}.andExpect {
					status { isOk() }
					header { string("Cache-Control", "no-store") }
					jsonPath("$.peerId") { value(peerId.toString()) }
					jsonPath("$.range") { value("HOUR") }
					jsonPath("$.points") { isArray() }
				}.andReturn()
		val metrics = objectMapper.readTree(metricsResponse.response.contentAsString)
		assertEquals(260, metrics["points"].sumOf { it["downloadBytes"].asLong() })
		assertEquals(150, metrics["points"].sumOf { it["uploadBytes"].asLong() })
		assertEquals(100, metrics["points"].sumOf { it["ruDownloadBytes"].asLong() })
		assertEquals(55, metrics["points"].sumOf { it["ruUploadBytes"].asLong() })
		assertEquals(160, metrics["points"].sumOf { it["nonRuDownloadBytes"].asLong() })
		assertEquals(95, metrics["points"].sumOf { it["nonRuUploadBytes"].asLong() })

		mockMvc
			.get("/api/admin/wireguard/relays/$relayId/peers/$peerId/metrics") {
				bearer(userToken)
				param("range", "MONTH")
			}.andExpect {
				status { isForbidden() }
			}
	}

	private fun createRelay(): JsonNode {
		val response =
			mockMvc
				.post("/api/admin/wireguard/relays") {
					admin()
					contentType = MediaType.APPLICATION_JSON
					content =
						"""{
						  "name":"Test relay ${UUID.randomUUID()}",
						  "publicEndpoint":"203.0.113.10:51820",
						  "clientCidr":"10.89.0.0/29",
						  "clientDns":"1.1.1.1"
						}""".trimIndent()
				}.andExpect {
					status { isCreated() }
					jsonPath("$.agentToken") { exists() }
				}.andReturn()
		val body = objectMapper.readTree(response.response.contentAsString)
		createdRelayIds.add(UUID.fromString(body["id"].asText()))
		return body
	}

	private fun createPeer(
		relayId: UUID,
		name: String,
	): JsonNode {
		val response =
			mockMvc
				.post("/api/admin/wireguard/relays/$relayId/peers") {
					admin()
					contentType = MediaType.APPLICATION_JSON
					content = objectMapper.writeValueAsString(mapOf("name" to name))
				}.andExpect {
					status { isCreated() }
				}.andReturn()
		return objectMapper.readTree(response.response.contentAsString)
	}

	private fun heartbeat(
		relayId: UUID,
		agentToken: String,
		appliedRevision: Long,
		counters: List<Counter>,
		routingStatus: Map<String, Any>? = null,
		routeQuality: Map<String, Any>? = null,
	) {
		val payload =
			linkedMapOf<String, Any>(
				"serverPublicKey" to serverPublicKey,
				"publicEndpoint" to "203.0.113.10:51820",
				"appliedRevision" to appliedRevision,
				"peers" to counters,
			)
		routingStatus?.let { payload["routingStatus"] = it }
		routeQuality?.let { payload["routeQuality"] = it }
		mockMvc
			.post("/api/internal/wireguard/relays/$relayId/heartbeat") {
				agent(agentToken)
				contentType = MediaType.APPLICATION_JSON
				content = objectMapper.writeValueAsString(payload)
			}.andExpect {
				status { isNoContent() }
			}
	}

	private fun createAndLogin(role: UserRole): String {
		val suffix = UUID.randomUUID().toString().take(8)
		val password = "password-123"
		val user =
			users.save(
				User(
					username = "wg-${role.name.lowercase()}-$suffix",
					email = "wg-${role.name.lowercase()}-$suffix@example.com",
					passwordHash = passwordEncoder.encode(password),
					role = role,
				),
			)
		createdUserIds.add(user.id)
		val login =
			mockMvc
				.post("/api/auth/login") {
					contentType = MediaType.APPLICATION_JSON
					content = objectMapper.writeValueAsString(mapOf("login" to user.username, "password" to password))
				}.andExpect {
					status { isOk() }
				}.andReturn()
		return objectMapper.readTree(login.response.contentAsString)["token"].asText()
	}

	private fun org.springframework.test.web.servlet.MockHttpServletRequestDsl.admin() = bearer(adminToken)

	private fun org.springframework.test.web.servlet.MockHttpServletRequestDsl.bearer(token: String) {
		header("Authorization", "Bearer $token")
	}

	private fun org.springframework.test.web.servlet.MockHttpServletRequestDsl.agent(token: String) {
		header("X-WireGuard-Agent-Token", token)
	}

	private data class Counter(
		val publicKey: String,
		val latestHandshakeEpochSeconds: Long,
		val receiveBytes: Long,
		val transmitBytes: Long,
		val routingTraffic: RoutingTraffic? = null,
	)

	private data class RoutingTraffic(
		val ruDownloadBytes: Long,
		val ruUploadBytes: Long,
		val nonRuDownloadBytes: Long,
		val nonRuUploadBytes: Long,
	)
}
