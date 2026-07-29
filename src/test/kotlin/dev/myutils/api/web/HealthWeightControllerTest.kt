package dev.myutils.api.web

import dev.myutils.api.properties.AppProperties
import dev.myutils.api.testkit.IntegrationTestBase
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.post
import java.time.LocalDate
import java.time.ZoneId

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

	@Test
	fun `imports apple shortcut multiline weights without dates from shortcut`() {
		val today = LocalDate.now(ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get()))
		val response =
			mockMvc
				.post("/api/health/weight/import") {
					contentType = MediaType.APPLICATION_JSON
					content = """{"":"83.1\n\n82,7 kg"}"""
				}.andReturn()
				.response

		assertEquals(200, response.status)
		assertTrue(response.contentAsString.contains("\"ok\":true"))
		assertTrue(response.contentAsString.contains("\"receivedDays\":3"))
		assertTrue(response.contentAsString.contains("\"savedDays\":2"))
		assertTrue(response.contentAsString.contains("\"latestDate\":\"$today\""))
		assertTrue(response.contentAsString.contains("\"latestWeightKg\":82.7"))
	}
}
