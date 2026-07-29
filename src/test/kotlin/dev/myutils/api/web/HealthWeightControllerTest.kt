package dev.myutils.api.web

import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.post

@AutoConfigureMockMvc
class HealthWeightControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Test
	fun `rejects payload with blank date`() {
		val response =
			mockMvc
				.post("/api/health/weight") {
					contentType = MediaType.APPLICATION_JSON
					content = """{"weightKg":83.1,"date":""}"""
				}.andReturn()
				.response

		assertEquals(400, response.status)
	}
}
