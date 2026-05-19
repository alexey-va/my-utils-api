package dev.myutils.api.agent

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import dev.myutils.api.openrouter.ToolCall
import dev.myutils.api.openrouter.ToolCallFunction
import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.temporal.TemporalNotificationFacade
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock
import org.mockito.kotlin.whenever

class WorkoutToolExecutorTest {
	private val objectMapper: ObjectMapper = jacksonObjectMapper()
	private val facade: WorkoutBotFacade = mock()
	private val notifications: TemporalNotificationFacade = mock()

	@Test
	fun `list_exercises delegates to facade`() {
		whenever(facade.listExercises()).thenReturn(emptyList())
		val executor = WorkoutToolExecutor(facade, notifications, objectMapper)
		val result =
			executor.execute(
				ToolCall(
					id = "1",
					function = ToolCallFunction(name = "list_exercises", arguments = "{}"),
				),
				chatId = 1L,
			)
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
		val executor = WorkoutToolExecutor(facade, notifications, objectMapper)
		val result =
			executor.execute(
				ToolCall(
					id = "2",
					function =
						ToolCallFunction(
							name = "log_workout",
							arguments =
								"""
								{
								  "exercise_name": "Bench press",
								  "weight_kg": 80,
								  "set_count": 3,
								  "reps_per_set": 5,
								  "max_reps": 5
								}
								""".trimIndent(),
						),
				),
				chatId = 1L,
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
		val executor = WorkoutToolExecutor(facade, notifications, objectMapper)
		val result =
			executor.execute(
				ToolCall(
					id = "3",
					function =
						ToolCallFunction(
							name = "rename_exercise",
							arguments =
								"""
								{
								  "current_name": "Жим",
								  "new_name": "Жим грудь"
								}
								""".trimIndent(),
						),
				),
				chatId = 1L,
			)
		assertTrue(result.contains("Переименовано"))
	}
}
