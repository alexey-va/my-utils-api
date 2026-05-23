package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.IntegrationTestBase
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.put
import kotlin.test.assertEquals

@AutoConfigureMockMvc
class AdminSettingsControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Test
	fun `update runtime property without auth`() {
		mockMvc
			.put("/api/admin/settings/openrouter.max-tool-iterations") {
				contentType = MediaType.APPLICATION_JSON
				content = objectMapper.writeValueAsString(mapOf("value" to 15))
			}.andExpect {
				status { isOk() }
				jsonPath("$.value") { value(15) }
			}

		val body =
			mockMvc
				.get("/api/admin/settings/openrouter.max-tool-iterations")
				.andExpect {
					status { isOk() }
				}.andReturn()
				.response.contentAsString

		assertEquals(15, objectMapper.readTree(body).get("value").asInt())
	}
}
