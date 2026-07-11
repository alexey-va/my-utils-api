package dev.myutils.api.service

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class WorkoutNotationParserTest {
	@Test
	fun `parses classic 3x max`() {
		val parsed = WorkoutNotationParser.parse("70 3*10/12")
		assertEquals(70, parsed.weightKg)
		assertEquals(listOf(10, 10, 10, 12), parsed.reps)
	}

	@Test
	fun `parses two sets without classic format`() {
		val parsed = WorkoutNotationParser.parse("70 8/12")
		assertEquals(listOf(8, 12), parsed.reps)
		assertEquals("70 кг 8/12", WorkoutSetReps.displayRu(parsed.weightKg, parsed.reps))
	}

	@Test
	fun `parses equal sets`() {
		val parsed = WorkoutNotationParser.parse("70 7/7/7")
		assertEquals(listOf(7, 7, 7), parsed.reps)
	}

	@Test
	fun `parses variable weights`() {
		val parsed = WorkoutNotationParser.parse("70/75/80 10/10/10")
		assertEquals(listOf(70, 75, 80), parsed.weights)
		assertEquals(listOf(10, 10, 10), parsed.reps)
	}

	@Test
	fun `rejects empty notation`() {
		assertThrows<IllegalArgumentException> { WorkoutNotationParser.parse("   ") }
	}
}
