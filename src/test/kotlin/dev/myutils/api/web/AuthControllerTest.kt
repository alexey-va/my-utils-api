package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.security.crypto.password.PasswordEncoder
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.post
import java.util.UUID

@AutoConfigureMockMvc
class AuthControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Autowired
	private lateinit var userRepository: UserRepository

	@Autowired
	private lateinit var passwordEncoder: PasswordEncoder

	@Test
	fun `login accepts seeded user email`() {
		mockMvc
			.post("/api/auth/login") {
				contentType = MediaType.APPLICATION_JSON
				content = json(mapOf("login" to "dev@example.com", "password" to "password"))
			}.andExpect {
				status { isOk() }
				jsonPath("$.token") { isNotEmpty() }
				jsonPath("$.user.username") { value("dev") }
				jsonPath("$.user.email") { value("dev@example.com") }
				jsonPath("$.user.role") { value("USER") }
			}
	}

	@Test
	fun `bootstrap admin can login by username and must change password`() {
		mockMvc
			.post("/api/auth/login") {
				contentType = MediaType.APPLICATION_JSON
				content = json(mapOf("login" to "freedeeml", "password" to "admin"))
			}.andExpect {
				status { isOk() }
				jsonPath("$.user.username") { value("freedeeml") }
				jsonPath("$.user.role") { value("ADMIN") }
				jsonPath("$.user.mustChangePassword") { value(true) }
			}
	}

	@Test
	fun `registration creates user session`() {
		val username = uniqueUsername()
		mockMvc
			.post("/api/auth/register") {
				contentType = MediaType.APPLICATION_JSON
				content =
					json(
						mapOf(
							"username" to username,
							"email" to "$username@example.com",
							"password" to "password-123",
						),
					)
			}.andExpect {
				status { isOk() }
				jsonPath("$.token") { isNotEmpty() }
				jsonPath("$.user.username") { value(username) }
				jsonPath("$.user.role") { value("USER") }
				jsonPath("$.user.mustChangePassword") { value(false) }
			}
	}

	@Test
	fun `registration rejects duplicate username`() {
		val username = uniqueUsername()
		register(username)

		mockMvc
			.post("/api/auth/register") {
				contentType = MediaType.APPLICATION_JSON
				content =
					json(
						mapOf(
							"username" to username.uppercase(),
							"email" to "other-$username@example.com",
							"password" to "password-123",
						),
					)
			}.andExpect {
				status { isConflict() }
				jsonPath("$.message") { value("Username is already taken") }
			}
	}

	@Test
	fun `login rejects bad password`() {
		mockMvc
			.post("/api/auth/login") {
				contentType = MediaType.APPLICATION_JSON
				content = json(mapOf("login" to "dev", "password" to "wrong"))
			}.andExpect {
				status { isUnauthorized() }
			}
	}

	@Test
	fun `me requires authentication`() {
		mockMvc.get("/api/auth/me").andExpect {
			status { isUnauthorized() }
		}
	}

	@Test
	fun `me returns full profile when authenticated`() {
		val token = login("dev@example.com", "password")

		mockMvc
			.get("/api/auth/me") {
				header("Authorization", "Bearer $token")
			}.andExpect {
				status { isOk() }
				jsonPath("$.username") { value("dev") }
				jsonPath("$.email") { value("dev@example.com") }
				jsonPath("$.role") { value("USER") }
			}
	}

	@Test
	fun `admin API requires admin role`() {
		mockMvc.get("/api/admin/settings").andExpect {
			status { isUnauthorized() }
		}

		val username = uniqueUsername()
		val userToken = register(username)
		mockMvc
			.get("/api/admin/settings") {
				header("Authorization", "Bearer $userToken")
			}.andExpect {
				status { isForbidden() }
			}

		val bootstrapAdminToken = login("freedeeml", "admin")
		mockMvc
			.get("/api/admin/settings") {
				header("Authorization", "Bearer $bootstrapAdminToken")
			}.andExpect {
				status { isForbidden() }
			}

		val admin = createReadyAdmin()
		val adminToken = login(admin.username, "password-123")
		mockMvc
			.get("/api/admin/settings") {
				header("Authorization", "Bearer $adminToken")
			}.andExpect {
				status { isOk() }
			}
	}

	@Test
	fun `credentials update changes username and password and revokes old session`() {
		val username = uniqueUsername()
		val oldToken = register(username)
		val newUsername = "${username}x"

		val response =
			mockMvc
				.post("/api/auth/credentials") {
					contentType = MediaType.APPLICATION_JSON
					header("Authorization", "Bearer $oldToken")
					content =
						json(
							mapOf(
								"currentPassword" to "password-123",
								"username" to newUsername,
								"newPassword" to "new-password-456",
							),
						)
				}.andExpect {
					status { isOk() }
					jsonPath("$.user.username") { value(newUsername) }
					jsonPath("$.token") { isNotEmpty() }
				}.andReturn()

		mockMvc
			.get("/api/auth/me") {
				header("Authorization", "Bearer $oldToken")
			}.andExpect {
				status { isUnauthorized() }
			}

		val newToken = objectMapper.readTree(response.response.contentAsString).get("token").asText()
		mockMvc
			.get("/api/auth/me") {
				header("Authorization", "Bearer $newToken")
			}.andExpect {
				status { isOk() }
				jsonPath("$.username") { value(newUsername) }
			}

		login(newUsername, "new-password-456")
	}

	@Test
	fun `logout revokes session`() {
		val token = login("dev", "password")

		mockMvc
			.post("/api/auth/logout") {
				header("Authorization", "Bearer $token")
			}.andExpect {
				status { isOk() }
			}

		mockMvc
			.get("/api/auth/me") {
				header("Authorization", "Bearer $token")
			}.andExpect {
				status { isUnauthorized() }
			}
	}

	private fun register(username: String): String {
		val result =
			mockMvc
				.post("/api/auth/register") {
					contentType = MediaType.APPLICATION_JSON
					content =
						json(
							mapOf(
								"username" to username,
								"email" to "$username@example.com",
								"password" to "password-123",
							),
						)
				}.andExpect {
					status { isOk() }
				}.andReturn()
		return objectMapper.readTree(result.response.contentAsString).get("token").asText()
	}

	private fun login(
		login: String,
		password: String,
	): String {
		val result =
			mockMvc
				.post("/api/auth/login") {
					contentType = MediaType.APPLICATION_JSON
					content = json(mapOf("login" to login, "password" to password))
				}.andExpect {
					status { isOk() }
				}.andReturn()
		return objectMapper.readTree(result.response.contentAsString).get("token").asText()
	}

	private fun json(value: Any): String = objectMapper.writeValueAsString(value)

	private fun uniqueUsername(): String = "user-${UUID.randomUUID().toString().take(8)}"

	private fun createReadyAdmin(): User {
		val username = "admin-${UUID.randomUUID().toString().take(8)}"
		return userRepository.save(
			User(
				username = username,
				email = "$username@example.com",
				passwordHash = passwordEncoder.encode("password-123"),
				role = UserRole.ADMIN,
				mustChangePassword = false,
			),
		)
	}
}
