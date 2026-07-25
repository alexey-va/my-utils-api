package dev.myutils.api.web.dto

import jakarta.validation.constraints.DecimalMin
import jakarta.validation.constraints.Min
import jakarta.validation.constraints.NotBlank
import jakarta.validation.constraints.NotNull
import java.time.LocalDate
import java.util.UUID

data class CreateExerciseRequest(
	@field:NotBlank val name: String,
	val muscleGroup: String? = null,
)

data class UpdateExerciseRequest(
	@field:NotBlank val name: String,
	val muscleGroup: String? = null,
)

data class ExerciseResponse(
	val id: UUID,
	val name: String,
	val muscleGroup: String,
)

data class UpsertWorkoutEntryRequest(
	@field:NotNull val exerciseId: UUID,
	@field:NotNull val performedOn: LocalDate,
	@field:DecimalMin("0.25") val weightKg: Double,
	@field:Min(1) val setCount: Int,
	@field:Min(1) val repsPerSet: Int,
	@field:Min(1) val maxReps: Int,
	val setReps: List<Int>? = null,
	val setWeights: List<Int>? = null,
)

data class WorkoutCellDto(
	val weightKg: Double,
	val setCount: Int,
	val repsPerSet: Int,
	val maxReps: Int,
	val setReps: List<Int>? = null,
	val display: String,
)

data class WorkoutGridRowDto(
	val exerciseId: UUID,
	val exerciseName: String,
	val cells: Map<String, WorkoutCellDto>,
)

data class WorkoutGridResponse(
	val dates: List<LocalDate>,
	val rows: List<WorkoutGridRowDto>,
)

data class ProgressPointDto(
	val date: LocalDate,
	val weightKg: Double,
	val setCount: Int,
	val repsPerSet: Int,
	val maxReps: Int,
	val setReps: List<Int>? = null,
	val volume: Double,
)

data class ExerciseStatsDto(
	val sessions: Int,
	val bestWeightKg: Double?,
	val latestWeightKg: Double?,
	val bestMaxReps: Int?,
	val bestVolume: Double?,
)

data class MoveWorkoutEntryRequest(
	@field:NotNull val fromExerciseId: UUID,
	@field:NotNull val fromDate: LocalDate,
	@field:NotNull val toExerciseId: UUID,
	@field:NotNull val toDate: LocalDate,
)

data class ExerciseProgressResponse(
	val exercise: ExerciseResponse,
	val points: List<ProgressPointDto>,
	val stats: ExerciseStatsDto,
)
