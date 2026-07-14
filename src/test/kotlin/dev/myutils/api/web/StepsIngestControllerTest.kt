package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.post

@AutoConfigureMockMvc
class StepsIngestControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Test
	fun `accepts steps payload and returns ok`() {
		val payload =
			mapOf(
				"date" to "2026-07-14",
				"steps" to 8432,
				"source" to "apple-shortcut",
			)

		val response =
			mockMvc
				.post("/api/health/steps") {
					contentType = MediaType.APPLICATION_JSON
					content = objectMapper.writeValueAsString(payload)
					header("X-Steps-Token", "test-token")
				}.andReturn()
				.response

		assertEquals(200, response.status)
		assertTrue(response.contentAsString.contains("\"ok\":true"))
		assertTrue(response.contentAsString.contains("8432"))
	}
}
