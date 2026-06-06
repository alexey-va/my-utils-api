package dev.myutils.api.agent

import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.temporal.TemporalNotificationFacade
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock
import org.mockito.kotlin.whenever

class WorkoutToolsServiceTest {
	private val facade: WorkoutBotFacade = mock()
	private val notifications: TemporalNotificationFacade = mock()

	@Test
	fun `list_exercises delegates to facade`() {
		whenever(facade.listExercises()).thenReturn(emptyList())
		val service = WorkoutToolsService(facade, notifications)
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
		val service = WorkoutToolsService(facade, notifications)
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
	fun `rename_exercise delegates to facade`() {
		whenever(
			facade.renameExercise(
				currentName = "Жим",
				newName = "Жим грудь",
				muscleGroup = null,
			),
		).thenReturn("Переименовано: «Жим» → «Жим грудь» (chest).")
		val service = WorkoutToolsService(facade, notifications)
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
