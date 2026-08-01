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
		val image = assertReportPng(png, expectedHeight = 1266)
		val tealPixels =
			countPixels(image) { color ->
				color.green > 110 && color.green > color.red + 35 && color.green > color.blue + 20
			}
		assertTrue(tealPixels > 20_000, "Expected prominent teal histogram bars")
	}

	@Test
	fun `renders weight as a clean filled trend`() {
		val points =
			(0L..11L).map { index ->
				WeeklyHealthReportRenderer.WeightPoint(
					date = to.minusDays(index * 4),
					weightKg = 83.1 + index * 0.1,
				)
			}

		val png = renderer.renderWeight(points, from, to)
		writePreview("weight.png", png)
		val image = assertReportPng(png, expectedHeight = 1266)
		val warmPixels =
			countPixels(image) { color ->
				color.red > 35 && color.red > color.blue + 10
			}
		assertTrue(warmPixels > 10_000, "Expected a warm gradient under the weight line, got $warmPixels pixels")
	}

	@Test
	fun `renders an informative empty report instead of failing`() {
		assertReportPng(renderer.renderSteps(emptyList(), from, to), expectedHeight = 1266)
		assertReportPng(renderer.renderWeight(emptyList(), from, to))
	}

	@Test
	fun `builds ten newest-first calendar rows for steps including missing days`() {
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
				DailyValueRow(to.minusDays(7), "—"),
				DailyValueRow(to.minusDays(8), "—"),
				DailyValueRow(to.minusDays(9), "—"),
			),
			rows,
		)
	}

	@Test
	fun `builds ten newest-first weight rows from actual measurements`() {
		val rows =
			latestWeightTableRows(
				points =
					(0L..11L).map { index ->
						WeeklyHealthReportRenderer.WeightPoint(
							date = to.minusDays(index * 3),
							weightKg = 82.0 + index * 0.1,
						)
					} + WeeklyHealthReportRenderer.WeightPoint(to.plusDays(1), 99.0),
				to = to,
			)

		assertEquals(
			listOf(
				DailyValueRow(to, "82,0 кг"),
				DailyValueRow(to.minusDays(3), "82,1 кг"),
				DailyValueRow(to.minusDays(6), "82,2 кг"),
				DailyValueRow(to.minusDays(9), "82,3 кг"),
				DailyValueRow(to.minusDays(12), "82,4 кг"),
				DailyValueRow(to.minusDays(15), "82,5 кг"),
				DailyValueRow(to.minusDays(18), "82,6 кг"),
				DailyValueRow(to.minusDays(21), "82,7 кг"),
				DailyValueRow(to.minusDays(24), "82,8 кг"),
				DailyValueRow(to.minusDays(27), "82,9 кг"),
			),
			rows,
		)
	}

	private fun assertReportPng(
		png: ByteArray,
		expectedHeight: Int = 1180,
	): java.awt.image.BufferedImage {
		assertTrue(png.size > 10_000)
		val image = ImageIO.read(ByteArrayInputStream(png))
		assertEquals(1200, image.width)
		assertEquals(expectedHeight, image.height)
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
