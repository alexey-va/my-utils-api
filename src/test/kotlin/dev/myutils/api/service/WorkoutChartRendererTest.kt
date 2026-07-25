package dev.myutils.api.service

import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.LocalDate

class WorkoutChartRendererTest {
	private val renderer = WorkoutChartRenderer()

	@Test
	fun `renderWeightProgress returns valid png`() {
		val points =
			listOf(
				WorkoutChartRenderer.Point(LocalDate.of(2026, 6, 1), 70.0, 10),
				WorkoutChartRenderer.Point(LocalDate.of(2026, 6, 8), 72.5, 11),
				WorkoutChartRenderer.Point(LocalDate.of(2026, 6, 15), 75.0, 9),
			)
		val png = renderer.renderWeightProgress("Жим лёжа", points)
		assertTrue(png.size > 1000)
		assertTrue(png[0] == 0x89.toByte())
		assertTrue(png[1] == 0x50.toByte())
		assertTrue(png[2] == 0x4E.toByte())
		assertTrue(png[3] == 0x47.toByte())
	}
}
