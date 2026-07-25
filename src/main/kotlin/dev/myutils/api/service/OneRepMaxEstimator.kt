package dev.myutils.api.service

import dev.myutils.api.domain.WorkoutEntry
import java.time.LocalDate
import kotlin.math.pow
import kotlin.math.round

/** Оценка одноповторного максимума (1ПМ) по подходам из дневника. */
object OneRepMaxEstimator {
	data class SetSample(
		val weightKg: Double,
		val reps: Int,
	)

	data class FormulaEstimate(
		val name: String,
		val oneRmKg: Double,
	)

	data class SessionEstimate(
		val date: LocalDate,
		val notation: String,
		val bestSet: SetSample,
		val formulas: List<FormulaEstimate>,
		val consensusKg: Double,
		val confidence: Confidence,
	)

	data class TrainingZone(
		val label: String,
		val percent: Int,
		val weightKg: Double,
	)

	data class Report(
		val exerciseName: String,
		val session: SessionEstimate,
		val historicalBestKg: Double?,
		val historicalBestDate: LocalDate?,
		val zones: List<TrainingZone>,
	)

	enum class Confidence {
		HIGH,
		MEDIUM,
		LOW,
	}

	fun estimateFromEntry(
		exerciseName: String,
		entry: WorkoutEntry,
		history: List<WorkoutEntry> = emptyList(),
	): Report {
		val session = estimateSession(entry)
		val historical = bestHistorical(history + entry)
		val zones = trainingZones(session.consensusKg)
		return Report(
			exerciseName = exerciseName,
			session = session,
			historicalBestKg = historical?.consensusKg,
			historicalBestDate = historical?.date,
			zones = zones,
		)
	}

	fun estimateSession(entry: WorkoutEntry): SessionEstimate {
		val sets = setsFromEntry(entry)
		require(sets.isNotEmpty()) { "Нет подходов для расчёта 1ПМ" }
		val ranked =
			sets
				.map { set -> set to formulaEstimates(set) }
				.filter { (_, formulas) -> formulas.isNotEmpty() }
				.maxByOrNull { (_, formulas) -> formulas.map { it.oneRmKg }.average() }
				?: throw IllegalArgumentException("Нет подходов с достаточным числом повторов")
		val (bestSet, formulas) = ranked
		val consensus = roundToStep(formulas.map { it.oneRmKg }.average(), 2.5)
		return SessionEstimate(
			date = entry.performedOn,
			notation = WorkoutNotation.format(entry),
			bestSet = bestSet,
			formulas = formulas,
			consensusKg = consensus,
			confidence = confidenceFor(bestSet.reps),
		)
	}

	fun setsFromEntry(entry: WorkoutEntry): List<SetSample> {
		val reps = WorkoutSetReps.effectiveReps(entry)
		val weights = WorkoutSetReps.effectiveWeights(entry)
		return reps.indices.map { index ->
			SetSample(
				weightKg = weights?.getOrNull(index)?.toDouble() ?: entry.weightKg,
				reps = reps[index],
			)
		}
	}

	fun formulaEstimates(set: SetSample): List<FormulaEstimate> {
		if (set.reps < 1) {
			return emptyList()
		}
		if (set.reps == 1) {
			return listOf(FormulaEstimate("Факт", set.weightKg))
		}
		val estimates = mutableListOf<FormulaEstimate>()
		estimates.add(FormulaEstimate("Эпли", epley(set.weightKg, set.reps)))
		brzycki(set.weightKg, set.reps)?.let { estimates.add(FormulaEstimate("Бржицки", it)) }
		estimates.add(FormulaEstimate("Ломбарди", lombardi(set.weightKg, set.reps)))
		wathan(set.weightKg, set.reps)?.let { estimates.add(FormulaEstimate("Ватан", it)) }
		return estimates
	}

	fun trainingZones(oneRmKg: Double): List<TrainingZone> =
		listOf(
			TrainingZone("Макс. сила", 90, roundToStep(oneRmKg * 0.90, 2.5)),
			TrainingZone("Сила", 85, roundToStep(oneRmKg * 0.85, 2.5)),
			TrainingZone("Тяжёлая сила", 80, roundToStep(oneRmKg * 0.80, 2.5)),
			TrainingZone("Гипертрофия", 75, roundToStep(oneRmKg * 0.75, 2.5)),
			TrainingZone("Объём", 70, roundToStep(oneRmKg * 0.70, 2.5)),
			TrainingZone("Техника", 60, roundToStep(oneRmKg * 0.60, 2.5)),
		)

	private fun bestHistorical(entries: List<WorkoutEntry>): SessionEstimate? =
		entries
			.mapNotNull { entry ->
				runCatching { estimateSession(entry) }.getOrNull()
			}.maxByOrNull { it.consensusKg }

	private fun confidenceFor(reps: Int): Confidence =
		when {
			reps == 1 -> Confidence.HIGH
			reps in 2..10 -> Confidence.HIGH
			reps in 11..15 -> Confidence.MEDIUM
			else -> Confidence.LOW
		}

	private fun epley(
		weightKg: Double,
		reps: Int,
	): Double = weightKg * (1.0 + reps / 30.0)

	private fun brzycki(
		weightKg: Double,
		reps: Int,
	): Double? {
		if (reps < 2 || reps >= 37) {
			return null
		}
		return weightKg * 36.0 / (37.0 - reps)
	}

	private fun lombardi(
		weightKg: Double,
		reps: Int,
	): Double = weightKg * reps.toDouble().pow(0.10)

	private fun wathan(
		weightKg: Double,
		reps: Int,
	): Double? {
		if (reps < 1) {
			return null
		}
		return (100.0 * weightKg) / (48.8 + 53.8 * kotlin.math.exp(-0.075 * reps))
	}

	private fun roundToStep(
		value: Double,
		step: Double,
	): Double = round(value / step) * step

	fun formatWeightKg(value: Double): String =
		if (value % 1.0 == 0.0) {
			value.toInt().toString()
		} else {
			"%.1f".format(value)
		}

	/** Текст для агента — что именно увидел пользователь на карточке. */
	fun formatAgentSummary(report: Report): String {
		val session = report.session
		val set = session.bestSet
		val formulas =
			session.formulas.joinToString { "${it.name} ${formatWeightKg(it.oneRmKg)}" }
		val zones =
			report.zones.joinToString { "${it.percent}% ${it.label} ${formatWeightKg(it.weightKg)} кг" }
		val historyLine =
			when {
				report.historicalBestKg == null -> "Рекорд по истории: первый расчёт"
				report.historicalBestKg <= session.consensusKg + 0.01 ->
					"Рекорд по истории: ${formatWeightKg(report.historicalBestKg)} кг (текущая сессия — рекорд)"
				else -> {
					val date = report.historicalBestDate?.format(java.time.format.DateTimeFormatter.ofPattern("dd.MM.yy")) ?: "—"
					"Рекорд по истории: ${formatWeightKg(report.historicalBestKg)} кг ($date)"
				}
			}
		return buildString {
			appendLine("Карточка 1ПМ отправлена в чат. Показано пользователю:")
			appendLine("• ${report.exerciseName}, сессия ${session.date.format(java.time.format.DateTimeFormatter.ofPattern("dd.MM.yy"))} — ${session.notation}")
			appendLine("• Оценка 1ПМ: ${formatWeightKg(session.consensusKg)} кг (${confidenceRu(session.confidence)})")
			appendLine("• Подход для расчёта: ${formatWeightKg(set.weightKg)}×${set.reps}")
			appendLine("• Формулы: $formulas")
			appendLine("• Рабочие зоны: $zones")
			appendLine("• $historyLine")
		}.trim()
	}

	private fun confidenceRu(confidence: Confidence): String =
		when (confidence) {
			Confidence.HIGH -> "высокая точность"
			Confidence.MEDIUM -> "средняя точность"
			Confidence.LOW -> "грубая оценка"
		}
}
