package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.post

@AutoConfigureMockMvc
class AuthControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Test
	fun `login returns token for seeded user`() {
		mockMvc
			.post("/api/auth/login") {
				contentType = MediaType.APPLICATION_JSON
				content =
					objectMapper.writeValueAsString(
						mapOf("email" to "dev@example.com", "password" to "password"),
					)
			}.andExpect {
				status { isOk() }
				jsonPath("$.token") { isNotEmpty() }
				jsonPath("$.user.email") { value("dev@example.com") }
			}
	}

	@Test
	fun `login rejects bad password`() {
		mockMvc
			.post("/api/auth/login") {
				contentType = MediaType.APPLICATION_JSON
				content =
					objectMapper.writeValueAsString(
						mapOf("email" to "dev@example.com", "password" to "wrong"),
					)
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
	fun `me returns profile when authenticated`() {
		val login =
			mockMvc
				.post("/api/auth/login") {
					contentType = MediaType.APPLICATION_JSON
					content =
						objectMapper.writeValueAsString(
							mapOf("email" to "dev@example.com", "password" to "password"),
						)
				}.andReturn()

		val token =
			objectMapper.readTree(login.response.contentAsString).get("token").asText()

		mockMvc
			.get("/api/auth/me") {
				header("Authorization", "Bearer $token")
			}.andExpect {
				status { isOk() }
				jsonPath("$.email") { value("dev@example.com") }
			}
	}

	@Test
	fun `logout revokes session`() {
		val login =
			mockMvc
				.post("/api/auth/login") {
					contentType = MediaType.APPLICATION_JSON
					content =
						objectMapper.writeValueAsString(
							mapOf("email" to "dev@example.com", "password" to "password"),
						)
				}.andReturn()

		val token =
			objectMapper.readTree(login.response.contentAsString).get("token").asText()

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
}
