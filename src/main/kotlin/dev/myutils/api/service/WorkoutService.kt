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
import org.slf4j.LoggerFactory
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
	private val log = LoggerFactory.getLogger(javaClass)

	fun listExercises(): List<ExerciseResponse> =
		exerciseRepository.findByUserIdOrderByNameAsc(localWorkoutUser().id).map(::toExerciseResponse)

	fun createExercise(
		request: CreateExerciseRequest,
		source: String = "api",
	): ExerciseResponse {
		val user = localWorkoutUser()
		val name = request.name.trim()
		if (name.isEmpty()) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Exercise name is required")
		}
		if (exerciseRepository.existsByUserIdAndNameIgnoreCase(user.id, name)) {
			throw ResponseStatusException(HttpStatus.CONFLICT, "Exercise already exists")
		}
		val exercise =
			exerciseRepository.save(
				Exercise(user = user, name = name, muscleGroup = normalizeMuscleGroup(request.muscleGroup)),
			)
		log.info(
			"DB INSERT exercise source={} user={} exerciseId={} name={} muscleGroup={}",
			source,
			user.email,
			exercise.id,
			exercise.name,
			exercise.muscleGroup,
		)
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
				.sortedDescending()

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
				val reps = WorkoutSetReps.effectiveReps(entry)
				ProgressPointDto(
					date = entry.performedOn,
					weightKg = entry.weightKg,
					setCount = entry.setCount,
					repsPerSet = entry.repsPerSet,
					maxReps = entry.maxReps,
					setReps = WorkoutSetReps.parseStorage(entry.setReps),
					volume = WorkoutSetReps.volume(entry),
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
		if (request.muscleGroup != null) {
			exercise.muscleGroup = normalizeMuscleGroup(request.muscleGroup)
		}
		return toExerciseResponse(exerciseRepository.save(exercise))
	}

	@Transactional
	fun deleteExercise(exerciseId: UUID) {
		val user = localWorkoutUser()
		findOwnedExercise(user, exerciseId)
		exerciseRepository.deleteById(exerciseId)
	}

	@Transactional
	fun deleteEntry(
		exerciseId: UUID,
		performedOn: LocalDate,
		source: String = "api",
	) {
		val user = localWorkoutUser()
		val exercise = findOwnedExercise(user, exerciseId)
		val existing =
			workoutEntryRepository.findByUserIdAndExerciseIdAndPerformedOn(
				user.id,
				exercise.id,
				performedOn,
			)
		if (existing.isEmpty) {
			throw ResponseStatusException(
				HttpStatus.NOT_FOUND,
				"No workout entry for ${exercise.name} on $performedOn",
			)
		}
		val entry = existing.get()
		workoutEntryRepository.delete(entry)
		log.info(
			"DB DELETE workout_entry source={} user={} entryId={} exerciseId={} exerciseName={} date={}",
			source,
			user.email,
			entry.id,
			exercise.id,
			exercise.name,
			performedOn,
		)
	}

	fun upsertEntry(
		request: UpsertWorkoutEntryRequest,
		source: String = "api",
	) {
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

		val replacedEntryId = existing.map { it.id }.orElse(null)
		if (existing.isPresent) {
			workoutEntryRepository.delete(existing.get())
		}

		val normalized =
			WorkoutSetReps.normalize(
				setCount = request.setCount,
				repsPerSet = request.repsPerSet,
				maxReps = request.maxReps,
				setReps = request.setReps,
			)

		val saved =
			workoutEntryRepository.save(
				WorkoutEntry(
					user = user,
					exercise = exercise,
					performedOn = request.performedOn,
					weightKg = request.weightKg,
					setCount = normalized.setCount,
					repsPerSet = normalized.repsPerSet,
					maxReps = normalized.maxReps,
					setReps = normalized.setRepsStorage,
				),
			)
		log.info(
			"DB UPSERT workout_entry source={} user={} action={} entryId={} replacedEntryId={} " +
				"exerciseId={} exerciseName={} date={} weightKg={} sets={} reps={} maxReps={} setReps={}",
			source,
			user.email,
			if (replacedEntryId != null) "replace" else "insert",
			saved.id,
			replacedEntryId,
			exercise.id,
			exercise.name,
			request.performedOn,
			request.weightKg,
			normalized.setCount,
			normalized.repsPerSet,
			normalized.maxReps,
			normalized.setRepsStorage,
		)
	}

	private fun toCellDto(entry: WorkoutEntry) =
		WorkoutCellDto(
			weightKg = entry.weightKg,
			setCount = entry.setCount,
			repsPerSet = entry.repsPerSet,
			maxReps = entry.maxReps,
			setReps = WorkoutSetReps.parseStorage(entry.setReps),
			display = formatCell(entry),
		)

	private fun formatCell(entry: WorkoutEntry): String = WorkoutSetReps.display(entry.weightKg, entry)

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
			bestVolume = entries.maxOf { WorkoutSetReps.volume(it) },
		)
	}

	private fun toExerciseResponse(exercise: Exercise) =
		ExerciseResponse(id = exercise.id, name = exercise.name, muscleGroup = exercise.muscleGroup)

	private fun normalizeMuscleGroup(raw: String?): String {
		val value = raw?.trim()?.lowercase() ?: "other"
		return if (value in ALLOWED_MUSCLE_GROUPS) value else "other"
	}

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

		private val ALLOWED_MUSCLE_GROUPS =
			setOf("chest", "back", "legs", "shoulders", "arms", "core", "other")
	}
}
