package dev.myutils.api.service

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.time.LocalDate

class WorkoutMuscleGroupsTest {
	@Test
	fun weekStartMonday_returnsMondayOfCurrentWeek() {
		// 2026-05-19 — вторник
		assertEquals(LocalDate.of(2026, 5, 18), WorkoutMuscleGroups.weekStartMonday(LocalDate.of(2026, 5, 19)))
	}

	@Test
	fun weekStartMonday_whenTodayIsMonday_returnsSameDay() {
		assertEquals(LocalDate.of(2026, 5, 18), WorkoutMuscleGroups.weekStartMonday(LocalDate.of(2026, 5, 18)))
	}
}
