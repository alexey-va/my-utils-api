package dev.myutils.api.agent

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class AgentToolArgumentsGroundingTest {
	@Test
	fun `converts workout pounds to rounded kilograms before tool execution`() {
		val grounded =
			AgentToolArgumentsGrounding.ground(
				toolName = "logWorkout",
				args = mapOf("exercise_name" to "Трицепс", "notation" to "71 3*12/15"),
				userMessage = "71 фунт трицепс 12/15",
			)

		assertEquals("32 3*12/15", grounded["notation"])
	}

	@Test
	fun `supports English pounds marker and decimal input`() {
		val grounded =
			AgentToolArgumentsGrounding.ground(
				toolName = "log_workout",
				args = mapOf("notation" to "100.5 3*10/12"),
				userMessage = "Трицепс 100,5 lbs 10/12",
			)

		assertEquals("46 3*10/12", grounded["notation"])
	}

	@Test
	fun `keeps existing kilogram literal grounding`() {
		val grounded =
			AgentToolArgumentsGrounding.ground(
				toolName = "log_workout",
				args = mapOf("notation" to "75 3*10/10"),
				userMessage = "сегодня 80 10/10",
			)

		assertEquals("80 10/10", grounded["notation"])
	}
}
