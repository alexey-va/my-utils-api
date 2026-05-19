package dev.myutils.api.service

import dev.myutils.api.domain.WorkoutEntry

/** Формат тренера: вес 3*X/МАХ (3 рабочих подхода по X повторов, 4-й подход — МАХ). */
object WorkoutNotation {
	fun format(
		weightKg: Int,
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int,
	): String = "$weightKg кг $setCount*$repsPerSet/$maxReps"

	fun format(entry: WorkoutEntry): String =
		format(entry.weightKg, entry.setCount, entry.repsPerSet, entry.maxReps)
}
