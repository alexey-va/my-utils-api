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
	private val from = LocalDate.of(2026, 5, 4)
	private val to = LocalDate.of(2026, 8, 1)

	@Test
	fun `renders steps as a clean histogram`() {
		val points =
			(0L..29L).map { day ->
				WeeklyHealthReportRenderer.StepPoint(
					date = to.minusDays(29).plusDays(day),
					steps = 6_000 + (day.toInt() % 7) * 1_100,
				)
			}

		val png = renderer.renderSteps(points, from, to)
		writePreview("steps.png", png)
		val image = assertReportPng(png)
		val tealPixels =
			countPixels(image) { color ->
				color.green > 110 && color.green > color.red + 35 && color.green > color.blue + 20
			}
		assertTrue(tealPixels > 20_000, "Expected prominent teal histogram bars")
	}

	@Test
	fun `renders weight as a clean filled trend`() {
		val points =
			listOf(
				WeeklyHealthReportRenderer.WeightPoint(from, 84.2),
				WeeklyHealthReportRenderer.WeightPoint(from.plusDays(20), 83.6),
				WeeklyHealthReportRenderer.WeightPoint(from.plusDays(50), 82.9),
				WeeklyHealthReportRenderer.WeightPoint(to, 83.1),
			)

		val png = renderer.renderWeight(points, from, to)
		writePreview("weight.png", png)
		val image = assertReportPng(png)
		val warmPixels =
			countPixels(image) { color ->
				color.red > 35 && color.red > color.blue + 10
			}
		assertTrue(warmPixels > 10_000, "Expected a warm gradient under the weight line, got $warmPixels pixels")
	}

	@Test
	fun `renders an informative empty report instead of failing`() {
		assertReportPng(renderer.renderSteps(emptyList(), from, to))
		assertReportPng(renderer.renderWeight(emptyList(), from, to))
	}

	@Test
	fun `builds seven newest-first calendar rows for steps including missing days`() {
		val rows =
			latestStepTableRows(
				points =
					listOf(
						WeeklyHealthReportRenderer.StepPoint(to, 12_345),
						WeeklyHealthReportRenderer.StepPoint(to.minusDays(2), 8_000),
					),
				to = to,
			)

		assertEquals(
			listOf(
				DailyValueRow(to, "12 345"),
				DailyValueRow(to.minusDays(1), "—"),
				DailyValueRow(to.minusDays(2), "8 000"),
				DailyValueRow(to.minusDays(3), "—"),
				DailyValueRow(to.minusDays(4), "—"),
				DailyValueRow(to.minusDays(5), "—"),
				DailyValueRow(to.minusDays(6), "—"),
			),
			rows,
		)
	}

	@Test
	fun `builds newest-first weight rows only for measured days in the latest week`() {
		val rows =
			latestWeightTableRows(
				points =
					listOf(
						WeeklyHealthReportRenderer.WeightPoint(to.minusDays(1), 82.35),
						WeeklyHealthReportRenderer.WeightPoint(to.minusDays(6), 83.0),
					),
				to = to,
			)

		assertEquals(
			listOf(
				DailyValueRow(to.minusDays(1), "82,4 кг"),
				DailyValueRow(to.minusDays(6), "83,0 кг"),
			),
			rows,
		)
	}

	private fun assertReportPng(png: ByteArray): java.awt.image.BufferedImage {
		assertTrue(png.size > 10_000)
		val image = ImageIO.read(ByteArrayInputStream(png))
		assertEquals(1200, image.width)
		assertEquals(1180, image.height)
		return image
	}

	private fun countPixels(
		image: java.awt.image.BufferedImage,
		matches: (java.awt.Color) -> Boolean,
	): Int {
		var count = 0
		for (y in 0 until image.height) {
			for (x in 0 until image.width) {
				if (matches(java.awt.Color(image.getRGB(x, y)))) {
					count++
				}
			}
		}
		return count
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
