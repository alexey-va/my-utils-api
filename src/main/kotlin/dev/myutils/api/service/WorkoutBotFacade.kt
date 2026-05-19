package dev.myutils.api.service

import dev.myutils.api.domain.Exercise
import dev.myutils.api.domain.ExerciseRepository
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.WorkoutEntryRepository
import dev.myutils.api.web.dto.CreateExerciseRequest
import dev.myutils.api.web.dto.ExerciseResponse
import dev.myutils.api.web.dto.UpdateExerciseRequest
import dev.myutils.api.web.dto.UpsertWorkoutEntryRequest
import org.slf4j.LoggerFactory
import org.springframework.http.HttpStatus
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

@Service
class WorkoutBotFacade(
	private val workoutService: WorkoutService,
	private val exerciseRepository: ExerciseRepository,
	private val workoutEntryRepository: WorkoutEntryRepository,
	private val userRepository: UserRepository,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val dateFmt = DateTimeFormatter.ofPattern("dd.MM.yyyy")

	fun listExercises(): List<ExerciseResponse> {
		val exercises = workoutService.listExercises()
		log.info("listExercises count={}", exercises.size)
		return exercises
	}

	fun createExercise(
		name: String,
		muscleGroup: String? = null,
	): String {
		val created =
			workoutService.createExercise(
				CreateExerciseRequest(name = name.trim(), muscleGroup = muscleGroup),
				source = "telegram-bot",
			)
		return "Создано упражнение «${created.name}» (${created.muscleGroup})."
	}

	fun renameExercise(
		currentName: String,
		newName: String,
		muscleGroup: String? = null,
	): String {
		val exercise = resolveExercise(currentName)
		val trimmed = newName.trim()
		if (trimmed.isEmpty()) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Новое название не может быть пустым")
		}
		val updated =
			workoutService.updateExercise(
				exercise.id,
				UpdateExerciseRequest(name = trimmed, muscleGroup = muscleGroup),
			)
		return "Переименовано: «${exercise.name}» → «${updated.name}» (${updated.muscleGroup})."
	}

	fun logWorkout(
		exerciseName: String,
		performedOn: LocalDate?,
		weightKg: Int,
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int?,
	): String {
		val exercise = resolveExercise(exerciseName)
		val date = performedOn ?: today()
		val max = maxReps ?: repsPerSet
		workoutService.upsertEntry(
			UpsertWorkoutEntryRequest(
				exerciseId = exercise.id,
				performedOn = date,
				weightKg = weightKg,
				setCount = setCount,
				repsPerSet = repsPerSet,
				maxReps = max,
			),
			source = "telegram-bot",
		)
		val notation = WorkoutNotation.format(weightKg, setCount, repsPerSet, max)
		return "Записано: ${exercise.name}, ${dateFmt.format(date)} — $notation"
	}

	fun deleteWorkout(
		exerciseName: String,
		performedOn: LocalDate?,
	): String {
		val exercise = resolveExercise(exerciseName)
		val date = performedOn ?: today()
		workoutService.deleteEntry(exercise.id, date, source = "telegram-bot")
		return "Удалено: ${exercise.name}, ${dateFmt.format(date)}"
	}

	@Transactional(readOnly = true)
	fun getExerciseProgressSummary(
		exerciseName: String,
		recentSessions: Int = 6,
	): String {
		val exercise = resolveExercise(exerciseName)
		val progress = workoutService.getProgress(exercise.id)
		val points = progress.points.takeLast(recentSessions.coerceIn(1, 20))
		val sb = StringBuilder()
		sb.appendLine("«${progress.exercise.name}» — прогресс")
		if (points.isEmpty()) {
			sb.append("Пока нет записей по этому упражнению.")
			return sb.toString().trim()
		}
		sb.appendLine("Последние тренировки:")
		for (p in points) {
			sb.appendLine(
				"• ${dateFmt.format(p.date)}: ${WorkoutNotation.format(p.weightKg, p.setCount, p.repsPerSet, p.maxReps)}",
			)
		}
		val stats = progress.stats
		sb.append(
			"Всего сессий: ${stats.sessions}. " +
				"Лучший вес: ${stats.bestWeightKg ?: "—"} кг. " +
				"Последний вес: ${stats.latestWeightKg ?: "—"} кг. " +
				"Лучший МАХ (4-й подход): ${stats.bestMaxReps ?: "—"}.",
		)
		return sb.toString().trim()
	}

	/** Компактный снимок для агента — пересобирается на каждое сообщение, не кешируется в Redis. */
	@Transactional(readOnly = true)
	fun buildAgentSnapshot(): String {
		val now = ZonedDateTime.now(USER_ZONE)
		val today = now.toLocalDate()
		val yesterday = today.minusDays(1)
		val user = localWorkoutUser()
		val exercises = exerciseRepository.findByUserIdOrderByNameAsc(user.id)
		val allEntries = workoutEntryRepository.findByUserIdOrderByPerformedOnDescCreatedAtDesc(user.id)
		val nowLine =
			"Сейчас: ${now.format(DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm"))} (Europe/Moscow)"

		return WorkoutAgentSnapshotFormatter.format(
			today = today,
			nowLine = nowLine,
			exercises = exercises,
			allEntries = allEntries,
			todaySummary = getDaySummary(today),
			yesterdaySummary = getDaySummary(yesterday),
		)
	}

	@Transactional(readOnly = true)
	fun getDaySummary(date: LocalDate? = null): String {
		val user = localWorkoutUser()
		val day = date ?: today()
		val entries = workoutEntryRepository.findByUserIdAndPerformedOnOrderByCreatedAtAsc(user.id, day)
		if (entries.isEmpty()) {
			return "За ${dateFmt.format(day)} записей нет."
		}
		val exerciseNames = exerciseRepository.findByUserIdOrderByNameAsc(user.id).associateBy { it.id }
		val sb = StringBuilder()
		sb.appendLine("Тренировка за ${dateFmt.format(day)}:")
		for (entry in entries) {
			val name = exerciseNames[entry.exercise.id]?.name ?: entry.exercise.name
			sb.appendLine("• $name: ${WorkoutNotation.format(entry)}")
		}
		sb.append("Всего упражнений: ${entries.size}.")
		return sb.toString().trim()
	}

	private fun today(): LocalDate = LocalDate.now(USER_ZONE)

	private fun resolveExercise(name: String): Exercise {
		val user = localWorkoutUser()
		val trimmed = name.trim()
		if (trimmed.isEmpty()) {
			throw ResponseStatusException(HttpStatus.BAD_REQUEST, "Exercise name is required")
		}

		val exact = exerciseRepository.findByUserIdAndNameIgnoreCase(user.id, trimmed)
		if (exact.isPresent) {
			return exact.get()
		}

		val all = exerciseRepository.findByUserIdOrderByNameAsc(user.id)
		val matches =
			all.filter { exercise ->
				exercise.name.equals(trimmed, ignoreCase = true) ||
					exercise.name.contains(trimmed, ignoreCase = true) ||
					trimmed.contains(exercise.name, ignoreCase = true)
			}

		return when {
			matches.size == 1 -> matches.first()
			matches.isEmpty() ->
				throw ResponseStatusException(
					HttpStatus.NOT_FOUND,
					"Нет упражнения «$trimmed». Сначала list_exercises или create_exercise.",
				)
			else ->
				throw ResponseStatusException(
					HttpStatus.CONFLICT,
					"Неоднозначно «$trimmed». Подходит: ${matches.joinToString { it.name }}",
				)
		}
	}

	private fun localWorkoutUser(): User =
		userRepository
			.findByEmailIgnoreCase(WorkoutService.LOCAL_WORKOUT_EMAIL)
			.orElseThrow {
				ResponseStatusException(
					HttpStatus.INTERNAL_SERVER_ERROR,
					"Local workout user is not configured",
				)
			}

	companion object {
		val USER_ZONE: ZoneId = ZoneId.of("Europe/Moscow")
	}
}
