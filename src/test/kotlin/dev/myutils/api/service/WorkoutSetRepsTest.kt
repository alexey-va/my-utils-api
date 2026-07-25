package dev.myutils.api.service

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class WorkoutSetRepsTest {
	@Test
	fun `parseArgument accepts slash and comma lists`() {
		assertEquals(listOf(10, 10, 9, 9), WorkoutSetReps.parseArgument("10/10/9/9"))
		assertEquals(listOf(10, 10, 9, 12), WorkoutSetReps.parseArgument("10,10,9,12"))
	}

	@Test
	fun `display variable reps`() {
		assertEquals("35  10/10/9/9", WorkoutSetReps.display(35, listOf(10, 10, 9, 9)))
	}

	@Test
	fun `display two sets without classic format`() {
		assertEquals("70 кг 8/12", WorkoutSetReps.displayRu(70, listOf(8, 12)))
	}

	@Test
	fun `display classic trainer pattern`() {
		assertEquals("70  3×10  (12)", WorkoutSetReps.display(70, listOf(10, 10, 10, 12)))
		assertEquals("70 кг 3*10/12", WorkoutSetReps.displayRu(70, listOf(10, 10, 10, 12)))
	}

	@Test
	fun `volume sums per-set reps`() {
		assertEquals(1330.0, WorkoutSetReps.volume(35, listOf(10, 10, 9, 9)))
	}

	@Test
	fun `legacy uniform has no storage`() {
		val normalized = WorkoutSetReps.normalize(setCount = 3, repsPerSet = 10, maxReps = 10, setReps = null)
		assertNull(normalized.setRepsStorage)
		assertEquals(listOf(10, 10, 10), normalized.reps)
	}

	@Test
	fun `legacy trainer stores explicit list`() {
		val normalized = WorkoutSetReps.normalize(setCount = 3, repsPerSet = 10, maxReps = 12, setReps = null)
		assertEquals("10,10,10,12", normalized.setRepsStorage)
	}

	@Test
	fun `parseArgument rejects invalid`() {
		assertThrows<IllegalArgumentException> { WorkoutSetReps.parseArgument("10/x") }
	}
}
