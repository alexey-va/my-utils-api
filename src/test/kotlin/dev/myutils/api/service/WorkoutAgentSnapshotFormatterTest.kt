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
	fun `includes only last N entries in recent section`() {
		val bench =
			Exercise(id = UUID.randomUUID(), user = user, name = "Жим", muscleGroup = "chest")
		val entries =
			(1..5).map { day ->
				WorkoutEntry(
					user = user,
					exercise = bench,
					performedOn = LocalDate.of(2026, 6, day),
					weightKg = 60 + day,
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
	}
}
