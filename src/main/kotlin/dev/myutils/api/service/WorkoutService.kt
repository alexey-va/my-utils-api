package dev.myutils.api.service

import dev.myutils.api.domain.Exercise
import dev.myutils.api.domain.ExerciseRepository
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.WorkoutEntry
import dev.myutils.api.domain.WorkoutEntryRepository
import dev.myutils.api.web.dto.CreateExerciseRequest
import dev.myutils.api.web.dto.UpdateExerciseRequest
import dev.myutils.api.web.dto.ExerciseResponse
import dev.myutils.api.web.dto.UpsertWorkoutEntryRequest
import dev.myutils.api.web.dto.WorkoutCellDto
import dev.myutils.api.web.dto.ExerciseProgressResponse
import dev.myutils.api.web.dto.ExerciseStatsDto
import dev.myutils.api.web.dto.ProgressPointDto
import dev.myutils.api.web.dto.WorkoutGridResponse
import dev.myutils.api.web.dto.WorkoutGridRowDto
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.util.UUID

@Service
class WorkoutService(
	private val userRepository: UserRepository,
	private val exerciseRepository: ExerciseRepository,
	private val workoutEntryRepository: WorkoutEntryRepository,
) {
	fun listExercises(): List<ExerciseResponse> =
		exerciseRepository.findByUserIdOrderByNameAsc(localWorkoutUser().id).map(::toExerciseResponse)

	fun createExercise(request: CreateExerciseRequest): ExerciseResponse {
		val user = localWorkoutUser()
		val name = request.name.trim()
		if (name.isEmpty()) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Exercise name is required")
		}
		if (exerciseRepository.existsByUserIdAndNameIgnoreCase(user.id, name)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Exercise already exists")
		}
		val exercise = exerciseRepository.save(Exercise(user = user, name = name))
		return toExerciseResponse(exercise)
	}

	fun getGrid(): WorkoutGridResponse {
		val user = localWorkoutUser()
		val exercises = exerciseRepository.findByUserIdOrderByNameAsc(user.id)
		val entries = workoutEntryRepository.findByUserIdOrderByPerformedOnDescCreatedAtDesc(user.id)

		val dates =
			entries
				.map { it.performedOn }
				.distinct()
				.sorted()

		val entryByExerciseAndDate =
			entries.associateBy { it.exercise.id to it.performedOn }

		val rows =
			exercises.map { exercise ->
				val cells =
					dates
						.mapNotNull { date ->
							val entry = entryByExerciseAndDate[exercise.id to date]
							if (entry != null) {
								date.toString() to toCellDto(entry)
							} else {
								null
							}
						}.toMap()
				WorkoutGridRowDto(
					exerciseId = exercise.id,
					exerciseName = exercise.name,
					cells = cells,
				)
			}

		return WorkoutGridResponse(dates = dates, rows = rows)
	}

	fun getProgress(exerciseId: UUID): ExerciseProgressResponse {
		val user = localWorkoutUser()
		val exercise = findOwnedExercise(user, exerciseId)
		val entries =
			workoutEntryRepository.findByUserIdAndExerciseIdOrderByPerformedOnAsc(user.id, exercise.id)

		val points =
			entries.map { entry ->
				ProgressPointDto(
					date = entry.performedOn,
					weightKg = entry.weightKg,
					setCount = entry.setCount,
					repsPerSet = entry.repsPerSet,
					maxReps = entry.maxReps,
					volume = entry.setCount * entry.repsPerSet * entry.weightKg,
				)
			}

		return ExerciseProgressResponse(
			exercise = toExerciseResponse(exercise),
			points = points,
			stats = buildStats(entries),
		)
	}

	fun updateExercise(exerciseId: UUID, request: UpdateExerciseRequest): ExerciseResponse {
		val user = localWorkoutUser()
		val exercise = findOwnedExercise(user, exerciseId)
		val name = request.name.trim()
		if (name.isEmpty()) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Exercise name is required")
		}
		if (
			exerciseRepository.existsByUserIdAndNameIgnoreCaseAndIdNot(user.id, name, exercise.id)
		) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Exercise already exists")
		}
		exercise.name = name
		return toExerciseResponse(exerciseRepository.save(exercise))
	}

	@Transactional
	fun deleteExercise(exerciseId: UUID) {
		val user = localWorkoutUser()
		findOwnedExercise(user, exerciseId)
		exerciseRepository.deleteById(exerciseId)
	}

	fun upsertEntry(request: UpsertWorkoutEntryRequest) {
		val user = localWorkoutUser()
		val exercise =
			exerciseRepository
				.findById(request.exerciseId)
				.filter { it.user.id == user.id }
				.orElseThrow { ResponseStatusException(HttpStatus.NOT_FOUND, "Exercise not found") }

		val existing =
			workoutEntryRepository.findByUserIdAndExerciseIdAndPerformedOn(
				user.id,
				exercise.id,
				request.performedOn,
			)

		if (existing.isPresent) {
			workoutEntryRepository.delete(existing.get())
		}

		workoutEntryRepository.save(
			WorkoutEntry(
				user = user,
				exercise = exercise,
				performedOn = request.performedOn,
				weightKg = request.weightKg,
				setCount = request.setCount,
				repsPerSet = request.repsPerSet,
				maxReps = request.maxReps,
			),
		)
	}

	private fun toCellDto(entry: WorkoutEntry) =
		WorkoutCellDto(
			weightKg = entry.weightKg,
			setCount = entry.setCount,
			repsPerSet = entry.repsPerSet,
			maxReps = entry.maxReps,
			display = formatCell(entry),
		)

	private fun formatCell(entry: WorkoutEntry): String =
		"${entry.weightKg}  ${entry.setCount}×${entry.repsPerSet}  (${entry.maxReps})"

	private fun findOwnedExercise(
		user: User,
		exerciseId: UUID,
	): Exercise =
		exerciseRepository
			.findById(exerciseId)
			.filter { it.user.id == user.id }
			.orElseThrow { ResponseStatusException(HttpStatus.NOT_FOUND, "Exercise not found") }

	private fun buildStats(entries: List<WorkoutEntry>): ExerciseStatsDto {
		if (entries.isEmpty()) {
			return ExerciseStatsDto(
				sessions = 0,
				bestWeightKg = null,
				latestWeightKg = null,
				bestMaxReps = null,
				bestVolume = null,
			)
		}
		return ExerciseStatsDto(
			sessions = entries.size,
			bestWeightKg = entries.maxOf { it.weightKg },
			latestWeightKg = entries.last().weightKg,
			bestMaxReps = entries.maxOf { it.maxReps },
			bestVolume = entries.maxOf { it.setCount * it.repsPerSet * it.weightKg },
		)
	}

	private fun toExerciseResponse(exercise: Exercise) =
		ExerciseResponse(id = exercise.id, name = exercise.name)

	private fun localWorkoutUser(): User =
		userRepository
			.findByEmailIgnoreCase(LOCAL_WORKOUT_EMAIL)
			.orElseThrow {
				ResponseStatusException(
					HttpStatus.INTERNAL_SERVER_ERROR,
					"Local workout user is not configured",
				)
			}

	companion object {
		const val LOCAL_WORKOUT_EMAIL = "local@workout"
	}
}
