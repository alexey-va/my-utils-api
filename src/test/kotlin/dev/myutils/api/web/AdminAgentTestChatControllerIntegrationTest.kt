package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.memory.AgentTestChatService
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.testkit.TestingIntegrationTestBase
import dev.myutils.api.testkit.impl.StubChatModelFactory
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
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
import java.util.UUID

@AutoConfigureMockMvc
class AdminAgentTestChatControllerIntegrationTest : TestingIntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Autowired
	private lateinit var users: UserRepository

	@Autowired
	private lateinit var passwordEncoder: PasswordEncoder

	@Autowired
	private lateinit var service: AgentTestChatService

	@Autowired
	private lateinit var chatModelFactory: StubChatModelFactory

	private lateinit var token: String
	private val createdChatIds = mutableListOf<UUID>()

	@BeforeEach
	fun loginAdmin() {
		val username = "agent-test-admin-${UUID.randomUUID().toString().take(8)}"
		users.save(
			User(
				username = username,
				email = "$username@example.com",
				passwordHash = passwordEncoder.encode("password-123"),
				role = UserRole.ADMIN,
			),
		)
		val login =
			mockMvc
				.post("/api/auth/login") {
					contentType = MediaType.APPLICATION_JSON
					content = objectMapper.writeValueAsString(mapOf("login" to username, "password" to "password-123"))
				}.andExpect {
					status { isOk() }
				}.andReturn()
		token = objectMapper.readTree(login.response.contentAsString).get("token").asText()
		chatModelFactory.model.resetResponses("Тестовый ответ.")
	}

	@AfterEach
	fun cleanChats() {
		createdChatIds.forEach { id ->
			runCatching { service.delete(id) }
		}
		createdChatIds.clear()
	}

	@Test
	fun `admin can create rename chat send message inspect history and clear it`() {
		val create =
			mockMvc
				.post("/api/admin/agent-test-chats") {
					admin()
					contentType = MediaType.APPLICATION_JSON
					content = """{"title":"Первый тест"}"""
				}.andExpect {
					status { isCreated() }
					jsonPath("$.title") { value("Первый тест") }
					jsonPath("$.messageCount") { value(0) }
				}.andReturn()
		val id = UUID.fromString(objectMapper.readTree(create.response.contentAsString).get("id").asText())
		createdChatIds.add(id)

		mockMvc
			.patch("/api/admin/agent-test-chats/$id") {
				admin()
				contentType = MediaType.APPLICATION_JSON
				content = """{"title":"Проверка tools"}"""
			}.andExpect {
				status { isOk() }
				jsonPath("$.title") { value("Проверка tools") }
			}

		mockMvc
			.post("/api/admin/agent-test-chats/$id/messages") {
				admin()
				contentType = MediaType.APPLICATION_JSON
				content = """{"content":"Привет"}"""
			}.andExpect {
				status { isOk() }
				jsonPath("$.reply") { value("Тестовый ответ.") }
				jsonPath("$.messages[0].role") { value("user") }
				jsonPath("$.messages[1].role") { value("assistant") }
			}

		val history =
			mockMvc
				.get("/api/admin/agent-test-chats/$id/messages?limit=50") {
					admin()
				}.andExpect {
					status { isOk() }
					jsonPath("$.messages.length()") { value(2) }
				}.andReturn()
		assertEquals(2, objectMapper.readTree(history.response.contentAsString).get("messages").size())

		mockMvc
			.delete("/api/admin/agent-test-chats/$id/messages") {
				admin()
			}.andExpect {
				status { isNoContent() }
			}

		mockMvc
			.get("/api/admin/agent-test-chats/$id") {
				admin()
			}.andExpect {
				status { isOk() }
				jsonPath("$.messageCount") { value(0) }
			}
	}

	@Test
	fun `test chat routes require admin and validate title`() {
		mockMvc
			.post("/api/admin/agent-test-chats") {
				contentType = MediaType.APPLICATION_JSON
				content = """{"title":"Нет токена"}"""
			}.andExpect {
				status { isUnauthorized() }
			}

		mockMvc
			.post("/api/admin/agent-test-chats") {
				admin()
				contentType = MediaType.APPLICATION_JSON
				content = """{"title":"   "}"""
			}.andExpect {
				status { isBadRequest() }
			}

		val created = service.create("Empty turn validation")
		createdChatIds.add(created.id)
		mockMvc
			.post("/api/admin/agent-test-chats/${created.id}/messages") {
				admin()
					contentType = MediaType.APPLICATION_JSON
					content = """{"content":"","images":[]}"""
			}.andExpect {
				status { isBadRequest() }
			}
	}

	private fun org.springframework.test.web.servlet.MockHttpServletRequestDsl.admin() {
		header("Authorization", "Bearer $token")
	}
}
