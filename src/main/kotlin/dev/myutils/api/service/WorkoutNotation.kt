package dev.myutils.api.service

import dev.myutils.api.domain.WorkoutEntry

/** Формат тренера: вес 3*X/МАХ или 35 кг 10/10/9/9. */
object WorkoutNotation {
	fun format(
		weightKg: Int,
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int,
	): String = WorkoutSetReps.displayRu(weightKg, WorkoutSetReps.normalize(setCount, repsPerSet, maxReps, null).reps)

	fun format(entry: WorkoutEntry): String = WorkoutSetReps.displayRu(entry.weightKg, WorkoutSetReps.effectiveReps(entry))
}
