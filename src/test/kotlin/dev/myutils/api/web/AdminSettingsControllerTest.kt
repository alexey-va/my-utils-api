package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.security.crypto.password.PasswordEncoder
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.post
import org.springframework.test.web.servlet.put
import kotlin.test.assertEquals

@AutoConfigureMockMvc
class AdminSettingsControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Autowired
	private lateinit var userRepository: UserRepository

	@Autowired
	private lateinit var passwordEncoder: PasswordEncoder

	@Test
	fun `admin can update runtime property`() {
		val username = "settings-admin-${java.util.UUID.randomUUID().toString().take(8)}"
		userRepository.save(
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
					content =
						objectMapper.writeValueAsString(
							mapOf("login" to username, "password" to "password-123"),
						)
				}.andExpect {
					status { isOk() }
				}.andReturn()
		val token = objectMapper.readTree(login.response.contentAsString).get("token").asText()

		mockMvc
			.put("/api/admin/settings/openrouter.max-tool-iterations") {
				header("Authorization", "Bearer $token")
				contentType = MediaType.APPLICATION_JSON
				content = objectMapper.writeValueAsString(mapOf("value" to 15))
			}.andExpect {
				status { isOk() }
				jsonPath("$.value") { value(15) }
			}

		val body =
			mockMvc
				.get("/api/admin/settings/openrouter.max-tool-iterations") {
					header("Authorization", "Bearer $token")
				}
				.andExpect {
					status { isOk() }
				}.andReturn()
				.response.contentAsString

		assertEquals(15, objectMapper.readTree(body).get("value").asInt())
	}

	@Test
	fun `admin can switch openrouter model through runtime settings API`() {
		val username = "model-settings-admin-${java.util.UUID.randomUUID().toString().take(8)}"
		userRepository.save(
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
					content =
						objectMapper.writeValueAsString(
							mapOf("login" to username, "password" to "password-123"),
						)
				}.andExpect {
					status { isOk() }
				}.andReturn()
		val token = objectMapper.readTree(login.response.contentAsString).get("token").asText()

		mockMvc
			.put("/api/admin/settings/openrouter.model") {
				header("Authorization", "Bearer $token")
				contentType = MediaType.APPLICATION_JSON
				content = objectMapper.writeValueAsString(mapOf("value" to "openai/gpt-5.4-mini"))
			}.andExpect {
				status { isOk() }
				jsonPath("$.value") { value("openai/gpt-5.4-mini") }
			}

		val body =
			mockMvc
				.get("/api/admin/settings/openrouter.model") {
					header("Authorization", "Bearer $token")
				}
				.andExpect {
					status { isOk() }
				}.andReturn()
				.response.contentAsString

		assertEquals("openai/gpt-5.4-mini", objectMapper.readTree(body).get("value").asText())
		assertEquals("openai/gpt-5.4-mini", AppProperties.OPENROUTER_MODEL.get())
	}
}
