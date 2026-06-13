package dev.myutils.api.agent

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ToolExecutionFeedbackTest {
	@Test
	fun `failure serializes result false`() {
		val json =
			ToolExecutionFeedback.failure(
				error = "Невалидный JSON аргументов",
				hint = "Даты в кавычках",
			)
		assertTrue(ToolExecutionFeedback.isFailure(json))
		assertTrue(json.contains("\"result\":false"))
		assertTrue(json.contains("Невалидный JSON аргументов"))
		assertTrue(json.contains("Даты в кавычках"))
	}

	@Test
	fun `success stays plain text`() {
		val text = ToolExecutionFeedback.success("день1\n\nдень2")
		assertEquals("день1\n\nдень2", text)
		assertFalse(ToolExecutionFeedback.isFailure(text))
	}
}
