package dev.myutils.api.service

import dev.myutils.api.domain.Exercise
import dev.myutils.api.domain.User
import dev.myutils.api.domain.WorkoutEntry
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.LocalDate
import java.util.UUID

class WorkoutAgentSnapshotFormatterTest {
	private val user = User(id = UUID.randomUUID(), email = "w@local", passwordHash = "x")

	@Test
	fun `exposes today and tomorrow with explicit weekdays`() {
		val snapshot =
			WorkoutAgentSnapshotFormatter.format(
				today = LocalDate.of(2026, 8, 1),
				nowLine = "Сейчас: 01.08.2026 17:13 (Europe/Moscow)",
				exercises = emptyList(),
				allEntries = emptyList(),
				recentEntriesLimit = 3,
				calendarDays = 14,
				progressSessionsPerExercise = 4,
				todaySummary = "Сегодня пусто",
				yesterdaySummary = "Вчера пусто",
			)

		assertTrue(
			snapshot.contains(
				"Сегодня: 2026-08-01 (суббота); завтра: 2026-08-02 (воскресенье); " +
					"неделя: 27.07–02.08 (понедельник–воскресенье)",
			),
		)
	}

	@Test
	fun `includes only last N entries in recent section`() {
		val bench =
			Exercise(id = UUID.randomUUID(), user = user, name = "Жим", muscleGroup = "chest")
		val entries =
			(1..5).map { day ->
				WorkoutEntry(
					user = user,
					exercise = bench,
					performedOn = LocalDate.of(2026, 6, day),
					weightKg = (60 + day).toDouble(),
					setCount = 3,
					repsPerSet = 8,
					maxReps = 10,
					createdAt = Instant.parse("2026-06-${day.toString().padStart(2, '0')}T12:00:00Z"),
				)
			}.sortedByDescending { it.performedOn }

		val snapshot =
			WorkoutAgentSnapshotFormatter.format(
				today = LocalDate.of(2026, 6, 5),
				nowLine = "Сейчас: 05.06.2026 12:00 (UTC)",
				exercises = listOf(bench),
				allEntries = entries,
				recentEntriesLimit = 3,
				calendarDays = 14,
				progressSessionsPerExercise = 4,
				todaySummary = "Сегодня ok",
				yesterdaySummary = "Вчера ok",
			)

		val recentBlock =
			snapshot
				.substringAfter("### Последние 3 записей дневника\n")
				.substringBefore("\n\n### ")
		assertEquals(3, recentBlock.lines().count { it.startsWith("•") })
		assertTrue(recentBlock.contains("05.06 «Жим»"))
		assertTrue(recentBlock.contains("03.06 «Жим»"))
		assertTrue(!recentBlock.contains("01.06 «Жим»"))
		assertTrue(snapshot.contains("### Календарь 14 дней"))
		assertTrue(snapshot.contains("### История по упражнениям"))
		assertTrue(snapshot.contains("### Список упражнений"))
	}
}
