package dev.myutils.api.service

import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.LocalDate

class WorkoutOneRmCardRendererTest {
	private val renderer = WorkoutOneRmCardRenderer()

	@Test
	fun `render returns valid png`() {
		val report =
			OneRepMaxEstimator.Report(
				exerciseName = "Жим лёжа",
				session =
					OneRepMaxEstimator.SessionEstimate(
						date = LocalDate.of(2026, 6, 15),
						notation = "75 кг 3*10/12",
						bestSet = OneRepMaxEstimator.SetSample(75, 12),
						formulas =
							listOf(
								OneRepMaxEstimator.FormulaEstimate("Эпли", 105.0),
								OneRepMaxEstimator.FormulaEstimate("Бржицки", 108.2),
								OneRepMaxEstimator.FormulaEstimate("Ломбарди", 103.5),
							),
						consensusKg = 107.5,
						confidence = OneRepMaxEstimator.Confidence.MEDIUM,
					),
				historicalBestKg = 107.5,
				historicalBestDate = LocalDate.of(2026, 6, 15),
				zones = OneRepMaxEstimator.trainingZones(107.5),
			)
		val png = renderer.render(report)
		assertTrue(png.size > 2000)
		assertTrue(png[0] == 0x89.toByte())
	}
}
