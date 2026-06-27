package dev.myutils.api.service

import dev.myutils.api.domain.WorkoutEntry

/** Повторы по подходам: uniform (legacy) или явный список, напр. 10/10/9/9. */
object WorkoutSetReps {
	data class Normalized(
		val setRepsStorage: String?,
		val setCount: Int,
		val repsPerSet: Int,
		val maxReps: Int,
		val reps: List<Int>,
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
	): Normalized {
		if (!setReps.isNullOrEmpty()) {
			val reps = setReps
			return Normalized(
				setRepsStorage = serialize(reps),
				setCount = reps.size,
				repsPerSet = reps.min(),
				maxReps = reps.max(),
				reps = reps,
			)
		}
		require(setCount >= 1) { "setCount ≥ 1" }
		require(repsPerSet >= 1) { "repsPerSet ≥ 1" }
		require(maxReps >= 1) { "maxReps ≥ 1" }
		val legacyReps = legacyRepsList(setCount, repsPerSet, maxReps)
		return Normalized(
			setRepsStorage = legacyReps?.let { serialize(it) },
			setCount = setCount,
			repsPerSet = repsPerSet,
			maxReps = maxReps,
			reps = legacyReps ?: List(setCount) { repsPerSet },
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

	fun volume(
		weightKg: Int,
		reps: List<Int>,
	): Int = weightKg * reps.sum()

	fun volume(entry: WorkoutEntry): Int = volume(entry.weightKg, effectiveReps(entry))

	fun display(
		weightKg: Int,
		entry: WorkoutEntry,
	): String = display(weightKg, effectiveReps(entry))

	fun display(
		weightKg: Int,
		reps: List<Int>,
	): String {
		if (reps.isEmpty()) {
			return "$weightKg"
		}
		val classic = classicTrainerDisplay(reps)
		if (classic != null) {
			return "$weightKg  $classic"
		}
		return "$weightKg  ${reps.joinToString("/")}"
	}

	fun displayRu(
		weightKg: Int,
		reps: List<Int>,
	): String {
		if (reps.isEmpty()) {
			return "$weightKg кг"
		}
		val classic = classicTrainerDisplay(reps)
		if (classic != null) {
			val working = reps.dropLast(1)
			return "$weightKg кг ${working.size}*${working.first()}/${reps.last()}"
		}
		return "$weightKg кг ${reps.joinToString("/")}"
	}

	private fun classicTrainerDisplay(reps: List<Int>): String? {
		if (reps.size < 2) {
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
