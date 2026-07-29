package dev.myutils.api.service

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.ByteArrayInputStream
import java.nio.file.Files
import java.nio.file.Path
import java.time.LocalDate
import javax.imageio.ImageIO

class WeeklyHealthReportRendererTest {
	private val renderer = WeeklyHealthReportRenderer()
	private val from = LocalDate.of(2026, 5, 1)
	private val to = LocalDate.of(2026, 7, 29)

	@Test
	fun `renders detailed steps png`() {
		val points =
			(0L..29L).map { day ->
				WeeklyHealthReportRenderer.StepPoint(
					date = to.minusDays(29).plusDays(day),
					steps = 6_000 + (day.toInt() % 7) * 1_100,
				)
			}

		val png = renderer.renderSteps(points, from, to)
		writePreview("steps.png", png)
		assertReportPng(png)
	}

	@Test
	fun `renders detailed sparse weight png`() {
		val points =
			listOf(
				WeeklyHealthReportRenderer.WeightPoint(from, 84.2),
				WeeklyHealthReportRenderer.WeightPoint(from.plusDays(20), 83.6),
				WeeklyHealthReportRenderer.WeightPoint(from.plusDays(50), 82.9),
				WeeklyHealthReportRenderer.WeightPoint(to, 83.1),
			)

		val png = renderer.renderWeight(points, from, to)
		writePreview("weight.png", png)
		assertReportPng(png)
	}

	@Test
	fun `renders an informative empty report instead of failing`() {
		assertReportPng(renderer.renderSteps(emptyList(), from, to))
		assertReportPng(renderer.renderWeight(emptyList(), from, to))
	}

	private fun assertReportPng(png: ByteArray) {
		assertTrue(png.size > 10_000)
		val image = ImageIO.read(ByteArrayInputStream(png))
		assertEquals(1200, image.width)
		assertEquals(760, image.height)
	}

	private fun writePreview(
		name: String,
		png: ByteArray,
	) {
		val previewDir = System.getenv("WEEKLY_REPORT_PREVIEW_DIR") ?: return
		val directory = Path.of(previewDir)
		Files.createDirectories(directory)
		Files.write(directory.resolve(name), png)
	}
}
