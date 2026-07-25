package dev.myutils.api.service

import dev.myutils.api.domain.WorkoutEntry

/** Повторы и веса по подходам: uniform (legacy) или явный список. */
object WorkoutSetReps {
	data class Normalized(
		val setRepsStorage: String?,
		val setWeightsStorage: String?,
		val setCount: Int,
		val repsPerSet: Int,
		val maxReps: Int,
		val reps: List<Int>,
		val weights: List<Int>?,
	)

	fun parseArgument(raw: String?): List<Int>? {
		val trimmed = raw?.trim().orEmpty()
		if (trimmed.isEmpty()) {
			return null
		}
		val parts =
			if (trimmed.contains("/")) {
				trimmed.split("/")
			} else {
				trimmed.split(",")
			}
		return parts.map { part ->
			part.trim().toIntOrNull()
				?: throw IllegalArgumentException("Некорректные повторы: «$raw»")
		}.also { reps ->
			require(reps.isNotEmpty()) { "Список повторов пуст" }
			require(reps.all { it >= 1 }) { "Каждый подход ≥ 1 повтора" }
		}
	}

	fun parseStorage(raw: String?): List<Int>? {
		if (raw.isNullOrBlank()) {
			return null
		}
		return raw.split(",").map { it.trim().toInt() }
	}

	fun serialize(reps: List<Int>): String = reps.joinToString(",")

	fun normalize(
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int,
		setReps: List<Int>?,
		setWeights: List<Int>? = null,
	): Normalized {
		if (!setReps.isNullOrEmpty()) {
			val reps = setReps
			val weights = setWeights?.takeIf { it.isNotEmpty() }
			return Normalized(
				setRepsStorage = serialize(reps),
				setWeightsStorage = weights?.let { serialize(it) },
				setCount = reps.size,
				repsPerSet = reps.min(),
				maxReps = reps.max(),
				reps = reps,
				weights = weights,
			)
		}
		require(setCount >= 1) { "setCount ≥ 1" }
		require(repsPerSet >= 1) { "repsPerSet ≥ 1" }
		require(maxReps >= 1) { "maxReps ≥ 1" }
		val legacyReps = legacyRepsList(setCount, repsPerSet, maxReps)
		return Normalized(
			setRepsStorage = legacyReps?.let { serialize(it) },
			setWeightsStorage = null,
			setCount = setCount,
			repsPerSet = repsPerSet,
			maxReps = maxReps,
			reps = legacyReps ?: List(setCount) { repsPerSet },
			weights = null,
		)
	}

	fun legacyRepsList(
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int,
	): List<Int>? {
		if (maxReps != repsPerSet) {
			return List(setCount) { repsPerSet } + maxReps
		}
		return null
	}

	fun effectiveReps(entry: WorkoutEntry): List<Int> {
		parseStorage(entry.setReps)?.let { return it }
		legacyRepsList(entry.setCount, entry.repsPerSet, entry.maxReps)?.let { return it }
		return List(entry.setCount) { entry.repsPerSet }
	}

	fun effectiveWeights(entry: WorkoutEntry): List<Int>? {
		parseStorage(entry.setWeights)?.let { return it }
		return null
	}

	fun volume(
		weightKg: Number,
		reps: List<Int>,
		weights: List<Int>? = null,
	): Double =
		if (!weights.isNullOrEmpty() && weights.size == reps.size) {
			weights.indices.sumOf { index -> weights[index].toDouble() * reps[index] }
		} else {
			weightKg.toDouble() * reps.sum()
		}

	fun volume(entry: WorkoutEntry): Double =
		volume(entry.weightKg, effectiveReps(entry), effectiveWeights(entry))

	fun display(
		weightKg: Number,
		entry: WorkoutEntry,
	): String = display(weightKg, effectiveReps(entry), effectiveWeights(entry))

	fun display(
		weightKg: Number,
		reps: List<Int>,
		weights: List<Int>? = null,
	): String {
		if (!weights.isNullOrEmpty() && weights.size == reps.size) {
			return "${weights.joinToString("/")}  ${reps.joinToString("/")}"
		}
		if (reps.isEmpty()) {
			return formatWeight(weightKg)
		}
		val classic = classicTrainerDisplay(reps)
		if (classic != null) {
			return "${formatWeight(weightKg)}  $classic"
		}
		return "${formatWeight(weightKg)}  ${reps.joinToString("/")}"
	}

	fun displayRu(
		weightKg: Number,
		reps: List<Int>,
		weights: List<Int>? = null,
	): String {
		if (!weights.isNullOrEmpty() && weights.size == reps.size) {
			return "${weights.joinToString("/")} кг ${reps.joinToString("/")}"
		}
		if (reps.isEmpty()) {
			return "${formatWeight(weightKg)} кг"
		}
		val classic = classicTrainerDisplay(reps)
		if (classic != null) {
			val working = reps.dropLast(1)
			return "${formatWeight(weightKg)} кг ${working.size}*${working.first()}/${reps.last()}"
		}
		return "${formatWeight(weightKg)} кг ${reps.joinToString("/")}"
	}

	fun formatWeight(weightKg: Number): String {
		val value = weightKg.toDouble()
		return if (value % 1.0 == 0.0) {
			value.toLong().toString()
		} else {
			java.math.BigDecimal.valueOf(value).stripTrailingZeros().toPlainString()
		}
	}

	private fun classicTrainerDisplay(reps: List<Int>): String? {
		if (reps.size < 3) {
			return null
		}
		val working = reps.dropLast(1)
		val max = reps.last()
		if (working.isEmpty() || !working.all { it == working.first() }) {
			return null
		}
		if (max == working.first()) {
			return null
		}
		return "${working.size}×${working.first()}  ($max)"
	}
}
