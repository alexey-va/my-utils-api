package dev.myutils.api.service

import dev.myutils.api.domain.Exercise
import dev.myutils.api.domain.ExerciseRepository
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.WorkoutEntry
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
	private val chartRenderer: WorkoutChartRenderer,
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
		notation: String,
	): String {
		val exercise = resolveExercise(exerciseName)
		val date = performedOn ?: today()
		val parsed = WorkoutNotationParser.parse(notation)
		workoutService.upsertEntry(
			UpsertWorkoutEntryRequest(
				exerciseId = exercise.id,
				performedOn = date,
				weightKg = parsed.weightKg,
				setCount = parsed.setCount,
				repsPerSet = parsed.repsPerSet,
				maxReps = parsed.maxReps,
				setReps = parsed.reps,
				setWeights = parsed.weights,
			),
			source = "telegram-bot",
		)
		val displayNotation =
			WorkoutSetReps.displayRu(parsed.weightKg, parsed.reps, parsed.weights)
		return "Записано: ${exercise.name}, ${dateFmt.format(date)} — $displayNotation"
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
	fun getExerciseProgressSummaries(
		exerciseNames: List<String>,
		recentSessions: Int = 6,
	): String {
		if (exerciseNames.isEmpty()) {
			return "Укажи exercises (названия через запятую)."
		}
		if (exerciseNames.size > MAX_EXERCISE_PROGRESS) {
			return "Слишком много упражнений (макс. $MAX_EXERCISE_PROGRESS)."
		}
		return exerciseNames
			.joinToString(separator = "\n\n") { name ->
				try {
					formatExerciseProgress(name, recentSessions)
				} catch (ex: ResponseStatusException) {
					ex.reason ?: "«$name»: ошибка"
				}
			}
	}

	private fun formatExerciseProgress(
		exerciseName: String,
		recentSessions: Int,
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

	@Transactional(readOnly = true)
	fun renderProgressChart(
		exerciseName: String,
		recentSessions: Int = 12,
	): Pair<ByteArray, String> {
		val exercise = resolveExercise(exerciseName)
		val progress = workoutService.getProgress(exercise.id)
		val limit = recentSessions.coerceIn(2, 30)
		val points =
			progress.points.takeLast(limit).map { point ->
				WorkoutChartRenderer.Point(
					date = point.date,
					weightKg = point.weightKg,
					maxReps = point.maxReps,
				)
			}
		if (points.size < 2) {
			throw ResponseStatusException(
				HttpStatus.BAD_REQUEST,
				"Мало данных для графика «${exercise.name}» (нужно ≥2 сессии, есть ${points.size}).",
			)
		}
		val png = chartRenderer.renderWeightProgress(exercise.name, points)
		val caption =
			"📈 <b>${exercise.name}</b> — вес и МАХ за ${points.size} сессий"
		return png to caption
	}

	/** Компактный снимок для агента — пересобирается на каждое сообщение, не кешируется в Redis. */
	@Transactional(readOnly = true)
	fun buildAgentSnapshot(): String {
		val zone = userZone()
		val now = ZonedDateTime.now(zone)
		val today = now.toLocalDate()
		val yesterday = today.minusDays(1)
		val user = localWorkoutUser()
		val exercises = exerciseRepository.findByUserIdOrderByNameAsc(user.id)
		val allEntries = workoutEntryRepository.findByUserIdOrderByPerformedOnDescCreatedAtDesc(user.id)
		val nowLine =
			"Сейчас: ${now.format(DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm"))} (${zone.id})"

		return WorkoutAgentSnapshotFormatter.format(
			today = today,
			nowLine = nowLine,
			exercises = exercises,
			allEntries = allEntries,
			recentEntriesLimit = AppProperties.AGENT_CONTEXT_RECENT_ENTRIES.get(),
			calendarDays = AppProperties.AGENT_CONTEXT_CALENDAR_DAYS.get(),
			progressSessionsPerExercise = AppProperties.AGENT_CONTEXT_PROGRESS_SESSIONS.get(),
			todaySummary = getDaySummary(today),
			yesterdaySummary = getDaySummary(yesterday),
		)
	}

	@Transactional(readOnly = true)
	fun hasWorkoutEntriesOn(date: LocalDate): Boolean {
		val user = localWorkoutUser()
		return workoutEntryRepository
			.findByUserIdAndPerformedOnOrderByCreatedAtAsc(user.id, date)
			.isNotEmpty()
	}

	@Transactional(readOnly = true)
	fun getDaySummary(date: LocalDate? = null): String {
		val user = localWorkoutUser()
		val day = date ?: today()
		val entries = workoutEntryRepository.findByUserIdAndPerformedOnOrderByCreatedAtAsc(user.id, day)
		val exerciseNames = exerciseRepository.findByUserIdOrderByNameAsc(user.id).associateBy { it.id }
		return formatDaySummary(day, entries, exerciseNames)
	}

	/** Сводки за несколько дней одним запросом (макс. 31 день). */
	@Transactional(readOnly = true)
	fun getDaySummaries(dates: List<LocalDate>): String {
		if (dates.isEmpty()) {
			return "Укажи from+to (YYYY-MM-DD) или days через запятую."
		}
		if (dates.size > MAX_DAY_SUMMARIES) {
			return "Слишком много дней (макс. $MAX_DAY_SUMMARIES). Сузь интервал."
		}
		val user = localWorkoutUser()
		val sorted = dates.distinct().sorted()
		val from = sorted.first()
		val to = sorted.last()
		val entries =
			workoutEntryRepository.findByUserIdAndPerformedOnBetweenOrderByPerformedOnAscCreatedAtAsc(
				user.id,
				from,
				to,
			)
		val exerciseNames = exerciseRepository.findByUserIdOrderByNameAsc(user.id).associateBy { it.id }
		val byDay = entries.groupBy { it.performedOn }
		return sorted
			.joinToString(separator = "\n\n") { day ->
				formatDaySummary(day, byDay[day].orEmpty(), exerciseNames)
			}
	}

	private fun formatDaySummary(
		day: LocalDate,
		entries: List<WorkoutEntry>,
		exerciseNames: Map<java.util.UUID, Exercise>,
	): String {
		if (entries.isEmpty()) {
			return "За ${dateFmt.format(day)} записей нет."
		}
		val sb = StringBuilder()
		sb.appendLine("Тренировка за ${dateFmt.format(day)}:")
		for (entry in entries) {
			val name = exerciseNames[entry.exercise.id]?.name ?: entry.exercise.name
			sb.appendLine("• $name: ${WorkoutNotation.format(entry)}")
		}
		sb.append("Всего упражнений: ${entries.size}.")
		return sb.toString().trim()
	}

	private fun today(): LocalDate = LocalDate.now(userZone())

	private fun userZone(): ZoneId = ZoneId.of(AppProperties.TEMPORAL_ZONE_ID.get())

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
		const val MAX_DAY_SUMMARIES = 31
		const val MAX_EXERCISE_PROGRESS = 15
	}
}
