package dev.myutils.api.service

import dev.myutils.api.domain.Exercise
import dev.myutils.api.domain.User
import dev.myutils.api.domain.WorkoutEntry
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.LocalDate
import java.util.UUID

class OneRepMaxEstimatorTest {
	private val user = User(id = UUID.randomUUID(), email = "u@test", passwordHash = "x")
	private val exercise = Exercise(id = UUID.randomUUID(), user = user, name = "Жим", muscleGroup = "chest")

	@Test
	fun `epley estimate for 100x5`() {
		val entry = entry(weight = 100, setCount = 3, repsPerSet = 5, maxReps = 5)
		val session = OneRepMaxEstimator.estimateSession(entry)
		val epley = session.formulas.first { it.name == "Эпли" }.oneRmKg
		assertEquals(116.7, epley, 0.1)
		assertEquals(100.0, session.bestSet.weightKg)
		assertEquals(5, session.bestSet.reps)
	}

	@Test
	fun `single rep returns actual weight`() {
		val entry = entry(weight = 140, setCount = 1, repsPerSet = 1, maxReps = 1)
		val session = OneRepMaxEstimator.estimateSession(entry)
		assertEquals(1, session.formulas.size)
		assertEquals(140.0, session.consensusKg, 0.01)
	}

	@Test
	fun `variable set weights use heaviest productive set`() {
		val entry =
			WorkoutEntry(
				user = user,
				exercise = exercise,
				performedOn = LocalDate.of(2026, 6, 1),
				weightKg = 70.0,
				setCount = 3,
				repsPerSet = 8,
				maxReps = 10,
				setReps = "8,8,10",
				setWeights = "70,75,75",
			)
		val session = OneRepMaxEstimator.estimateSession(entry)
		assertEquals(75.0, session.bestSet.weightKg)
		assertEquals(10, session.bestSet.reps)
	}

	@Test
	fun `training zones are rounded to 2_5 kg`() {
		val zones = OneRepMaxEstimator.trainingZones(100.0)
		assertEquals(90.0, zones.first { it.percent == 90 }.weightKg)
		assertEquals(75.0, zones.first { it.percent == 75 }.weightKg)
	}

	@Test
	fun `report tracks historical best`() {
		val old = entry(weight = 70, setCount = 3, repsPerSet = 10, maxReps = 12, date = LocalDate.of(2026, 5, 1))
		val recent = entry(weight = 80, setCount = 3, repsPerSet = 8, maxReps = 10, date = LocalDate.of(2026, 6, 1))
		val report =
			OneRepMaxEstimator.estimateFromEntry(
				exerciseName = "Жим",
				entry = recent,
				history = listOf(old, recent),
			)
		assertTrue(report.historicalBestKg != null)
		assertTrue(report.session.consensusKg > 0)
	}

	@Test
	fun `formatAgentSummary includes key numbers`() {
		val report =
			OneRepMaxEstimator.Report(
				exerciseName = "Жим",
				session =
					OneRepMaxEstimator.SessionEstimate(
						date = LocalDate.of(2026, 6, 15),
						notation = "75 кг 3*10/12",
						bestSet = OneRepMaxEstimator.SetSample(75.0, 12),
						formulas = listOf(OneRepMaxEstimator.FormulaEstimate("Эпли", 105.0)),
						consensusKg = 107.5,
						confidence = OneRepMaxEstimator.Confidence.MEDIUM,
					),
				historicalBestKg = 107.5,
				historicalBestDate = LocalDate.of(2026, 6, 15),
				zones = OneRepMaxEstimator.trainingZones(107.5),
			)
		val summary = OneRepMaxEstimator.formatAgentSummary(report)
		assertTrue(summary.contains("107.5"))
		assertTrue(summary.contains("75×12"))
		assertTrue(summary.contains("Рабочие зоны"))
		assertTrue(summary.contains("Показано пользователю"))
	}

	private fun entry(
		weight: Int,
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int,
		date: LocalDate = LocalDate.of(2026, 6, 15),
	): WorkoutEntry =
		WorkoutEntry(
			user = user,
			exercise = exercise,
			performedOn = date,
				weightKg = weight.toDouble(),
			setCount = setCount,
			repsPerSet = repsPerSet,
			maxReps = maxReps,
		)
}
