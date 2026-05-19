package dev.myutils.api.web

import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.IntegrationTestBase
import org.junit.jupiter.api.Test
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc
import org.springframework.http.MediaType
import org.springframework.test.web.servlet.MockMvc
import org.springframework.test.web.servlet.delete
import org.springframework.test.web.servlet.get
import org.springframework.test.web.servlet.post
import kotlin.test.assertTrue

@AutoConfigureMockMvc
class WorkoutControllerTest : IntegrationTestBase() {
	@Autowired
	private lateinit var mockMvc: MockMvc

	@Autowired
	private lateinit var objectMapper: ObjectMapper

	@Test
	fun `grid dates are oldest to newest`() {
		val exerciseName = "Grid sort lift ${System.nanoTime()}"
		val exercise =
			mockMvc
				.post("/api/workouts/exercises") {
					contentType = MediaType.APPLICATION_JSON
					content = objectMapper.writeValueAsString(mapOf("name" to exerciseName))
				}.andExpect {
					status { isOk() }
				}.andReturn()

		val exerciseId = objectMapper.readTree(exercise.response.contentAsString).get("id").asText()

		for ((day, weight) in listOf("2026-05-18" to 70, "2026-05-19" to 75, "2026-05-20" to 80)) {
			mockMvc
				.post("/api/workouts/entries") {
					contentType = MediaType.APPLICATION_JSON
					content =
						objectMapper.writeValueAsString(
							mapOf(
								"exerciseId" to exerciseId,
								"performedOn" to day,
								"weightKg" to weight,
								"setCount" to 3,
								"repsPerSet" to 10,
								"maxReps" to 12,
							),
						)
				}.andExpect {
					status { isNoContent() }
				}
		}

		val grid =
			mockMvc
				.get("/api/workouts/grid")
				.andExpect {
					status { isOk() }
				}.andReturn()

		val dates =
			objectMapper
				.readTree(grid.response.contentAsString)
				.get("dates")
				.map { it.asText() }

		assertTrue(dates == dates.sorted())
		val i18 = dates.indexOf("2026-05-18")
		val i19 = dates.indexOf("2026-05-19")
		val i20 = dates.indexOf("2026-05-20")
		assertTrue(i18 >= 0 && i19 > i18 && i20 > i19)

		mockMvc.delete("/api/workouts/exercises/$exerciseId").andExpect {
			status { isNoContent() }
		}
	}

	@Test
	fun `exercise progress and delete`() {
		val exerciseName = "Progress lift ${System.nanoTime()}"
		val exercise =
			mockMvc
				.post("/api/workouts/exercises") {
					contentType = MediaType.APPLICATION_JSON
					content = objectMapper.writeValueAsString(mapOf("name" to exerciseName))
				}.andExpect {
					status { isOk() }
				}.andReturn()

		val exerciseId = objectMapper.readTree(exercise.response.contentAsString).get("id").asText()

		mockMvc
			.post("/api/workouts/entries") {
				contentType = MediaType.APPLICATION_JSON
				content =
					objectMapper.writeValueAsString(
						mapOf(
							"exerciseId" to exerciseId,
							"performedOn" to "2026-05-10",
							"weightKg" to 70,
							"setCount" to 3,
							"repsPerSet" to 8,
							"maxReps" to 10,
						),
					)
			}.andExpect {
				status { isNoContent() }
			}

		mockMvc
			.post("/api/workouts/entries") {
				contentType = MediaType.APPLICATION_JSON
				content =
					objectMapper.writeValueAsString(
						mapOf(
							"exerciseId" to exerciseId,
							"performedOn" to "2026-05-18",
							"weightKg" to 80,
							"setCount" to 3,
							"repsPerSet" to 10,
							"maxReps" to 12,
						),
					)
			}.andExpect {
				status { isNoContent() }
			}

		mockMvc.get("/api/workouts/exercises/$exerciseId/progress").andExpect {
			status { isOk() }
			jsonPath("$.stats.sessions") { value(2) }
			jsonPath("$.stats.bestWeightKg") { value(80) }
			jsonPath("$.points.length()") { value(2) }
		}

		mockMvc.delete("/api/workouts/exercises/$exerciseId").andExpect {
			status { isNoContent() }
		}

		mockMvc.get("/api/workouts/exercises").andExpect {
			status { isOk() }
			jsonPath("$[?(@.id == '$exerciseId')]") { isEmpty() }
		}
	}
}
