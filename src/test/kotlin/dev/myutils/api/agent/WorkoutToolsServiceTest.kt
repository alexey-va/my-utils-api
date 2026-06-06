package dev.myutils.api.agent

import dev.myutils.api.infra.observability.AgentMetrics
import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.temporal.TemporalNotificationFacade
import io.micrometer.core.instrument.simple.SimpleMeterRegistry
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock
import org.mockito.kotlin.whenever

class WorkoutToolsServiceTest {
	private val facade: WorkoutBotFacade = mock()
	private val notifications: TemporalNotificationFacade = mock()

	private fun service(): WorkoutToolsService =
		WorkoutToolsService(facade, notifications, AgentMetrics(SimpleMeterRegistry()))

	@Test
	fun `list_exercises delegates to facade`() {
		whenever(facade.listExercises()).thenReturn(emptyList())
		val service = service()
		val result = service.runTool("list_exercises", chatId = 1L, args = emptyMap())
		assertTrue(result.contains("Упражнений пока нет"))
	}

	@Test
	fun `log_workout parses arguments`() {
		whenever(
			facade.logWorkout(
				exerciseName = "Bench press",
				performedOn = null,
				weightKg = 80,
				setCount = 3,
				repsPerSet = 5,
				maxReps = 5,
			),
		).thenReturn("Записано: Bench press")
		val service = service()
		val result =
			service.runTool(
				"log_workout",
				chatId = 1L,
				args =
					mapOf(
						"exercise_name" to "Bench press",
						"weight_kg" to "80",
						"set_count" to "3",
						"reps_per_set" to "5",
						"max_reps" to "5",
					),
			)
		assertTrue(result.contains("Записано"))
	}

	@Test
	fun `logWorkout accepts LangChain4j camelCase tool name and args`() {
		whenever(
			facade.logWorkout(
				exerciseName = "Жим",
				performedOn = null,
				weightKg = 70,
				setCount = 3,
				repsPerSet = 10,
				maxReps = 12,
			),
		).thenReturn("Записано: Жим")
		val service = service()
		val result =
			service.runTool(
				"logWorkout",
				chatId = 1L,
				args =
					mapOf(
						"exerciseName" to "Жим",
						"weightKg" to "70",
						"setCount" to "3",
						"repsPerSet" to "10",
						"maxReps" to "12",
					),
			)
		assertTrue(result.contains("Записано"))
	}

	@Test
	fun `rename_exercise delegates to facade`() {
		whenever(
			facade.renameExercise(
				currentName = "Жим",
				newName = "Жим грудь",
				muscleGroup = null,
			),
		).thenReturn("Переименовано: «Жим» → «Жим грудь» (chest).")
		val service = service()
		val result =
			service.runTool(
				"rename_exercise",
				chatId = 1L,
				args =
					mapOf(
						"current_name" to "Жим",
						"new_name" to "Жим грудь",
					),
			)
		assertTrue(result.contains("Переименовано"))
	}
}
