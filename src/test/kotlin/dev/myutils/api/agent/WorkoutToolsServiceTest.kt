package dev.myutils.api.agent

import dev.myutils.api.infra.observability.AgentMetrics
import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.temporal.TemporalNotificationFacade
import org.springframework.beans.factory.ObjectProvider
import io.micrometer.core.instrument.simple.SimpleMeterRegistry
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import java.time.LocalDate
import org.junit.jupiter.api.Test
import org.mockito.kotlin.eq
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

class WorkoutToolsServiceTest {
	private val facade: WorkoutBotFacade = mock()
	private val notifications: TemporalNotificationFacade = mock()

	private fun service(messenger: TelegramMessenger? = null): WorkoutToolsService {
		val messengerProvider = mock<ObjectProvider<TelegramMessenger>>()
		whenever(messengerProvider.getIfAvailable()).thenReturn(messenger)
		return WorkoutToolsService(facade, notifications, AgentMetrics(SimpleMeterRegistry()), messengerProvider)
	}

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

	@Test
	fun `get_day_summaries interval delegates to facade`() {
		val dates = listOf(LocalDate.parse("2026-06-01"), LocalDate.parse("2026-06-02"))
		whenever(facade.getDaySummaries(dates)).thenReturn("день1\n\nдень2")
		val service = service()
		val result =
			service.runTool(
				"getDaySummaries",
				chatId = 1L,
				args = mapOf("from" to "2026-06-01", "to" to "2026-06-02"),
			)
		assertEquals("день1\n\nдень2", result)
	}

	@Test
	fun `get_day_summaries days list delegates to facade`() {
		val dates =
			listOf(
				LocalDate.parse("2026-06-04"),
				LocalDate.parse("2026-06-01"),
			)
		whenever(facade.getDaySummaries(dates)).thenReturn("сводки")
		val service = service()
		val result =
			service.runTool(
				"get_day_summaries",
				chatId = 1L,
				args = mapOf("days" to "2026-06-04,2026-06-01"),
			)
		assertEquals("сводки", result)
	}

	@Test
	fun `resolveDayList expands inclusive interval`() {
		val service = service()
		val resolved = service.resolveDayList("2026-06-01", "2026-06-03", null)
		assertTrue(resolved is WorkoutToolsService.DayListResolve.Ok)
		val ok = resolved as WorkoutToolsService.DayListResolve.Ok
		assertEquals(
			listOf(
				LocalDate.parse("2026-06-01"),
				LocalDate.parse("2026-06-02"),
				LocalDate.parse("2026-06-03"),
			),
			ok.dates,
		)
	}

	@Test
	fun `resolveDayList rejects interval without both ends`() {
		val service = service()
		val resolved = service.resolveDayList("2026-06-01", null, null)
		assertTrue(resolved is WorkoutToolsService.DayListResolve.Error)
	}

	@Test
	fun `resolveDayList accepts single day in days list`() {
		val service = service()
		val resolved = service.resolveDayList(null, null, "2026-06-04")
		assertTrue(resolved is WorkoutToolsService.DayListResolve.Ok)
		val ok = resolved as WorkoutToolsService.DayListResolve.Ok
		assertEquals(listOf(LocalDate.parse("2026-06-04")), ok.dates)
	}

	@Test
	fun `get_exercise_progresses delegates to facade`() {
		val names = listOf("Жим", "Присед")
		whenever(facade.getExerciseProgressSummaries(names, 6)).thenReturn("прогресс")
		val service = service()
		val result =
			service.runTool(
				"getExerciseProgresses",
				chatId = 1L,
				args = mapOf("exercises" to "Жим,Присед"),
			)
		assertEquals("прогресс", result)
	}

	@Test
	fun `resolveExerciseList parses comma-separated names`() {
		val service = service()
		val resolved = service.resolveExerciseList("Жим, Присед")
		assertTrue(resolved is WorkoutToolsService.ExerciseListResolve.Ok)
		val ok = resolved as WorkoutToolsService.ExerciseListResolve.Ok
		assertEquals(listOf("Жим", "Присед"), ok.names)
	}

	@Test
	fun `send_rich_message sends html with buttons`() {
		val messenger: TelegramMessenger = mock()
		val service = service(messenger)
		val result =
			service.runTool(
				"send_rich_message",
				chatId = 42L,
				args =
					mapOf(
						"text" to "<b>План</b>",
						"buttons" to "Сегодня:что на сегодня",
					),
			)
		assertTrue(result.contains("кнопками"))
		verify(messenger).sendHtmlMessage(eq(42L), eq("<b>План</b>"), org.mockito.kotlin.any())
	}

	@Test
	fun `send_rich_message without telegram returns fallback`() {
		val service = service(messenger = null)
		val result =
			service.runTool(
				"send_rich_message",
				chatId = 1L,
				args = mapOf("text" to "hi"),
			)
		assertTrue(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Telegram недоступен"))
	}

	@Test
	fun `get_day_summaries rejects invalid date without guessing`() {
		val service = service()
		val result =
			service.runTool(
				"getDaySummaries",
				chatId = 1L,
				args = mapOf("from" to "20226-06-09", "to" to "2026-06-09"),
			)
		assertTrue(ToolExecutionFeedback.isFailure(result))
		assertTrue(result.contains("Неверная дата from"))
		assertTrue(result.contains("20226-06-09"))
	}

	@Test
	fun `unknown single tools return error`() {
		val service = service()
		assertTrue(ToolExecutionFeedback.isFailure(service.runTool("getDaySummary", 1L, emptyMap())))
		assertTrue(
			service.runTool("getDaySummary", 1L, emptyMap()).contains("Неизвестный инструмент"),
		)
		assertTrue(ToolExecutionFeedback.isFailure(service.runTool("getExerciseProgress", 1L, emptyMap())))
	}
}
