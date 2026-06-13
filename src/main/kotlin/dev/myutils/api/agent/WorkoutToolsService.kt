package dev.myutils.api.agent

import dev.myutils.api.infra.observability.AgentMetrics
import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.service.WorkoutBotFacade.Companion.MAX_DAY_SUMMARIES
import dev.myutils.api.service.WorkoutBotFacade.Companion.MAX_EXERCISE_PROGRESS
import dev.myutils.api.telegram.TelegramButtonParser
import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.temporal.TemporalNotificationFacade
import dev.myutils.api.infra.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.stereotype.Service
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.time.format.DateTimeParseException
import java.time.temporal.ChronoUnit

@Service
class WorkoutToolsService(
	private val workoutBotFacade: WorkoutBotFacade,
	private val temporalNotificationFacade: TemporalNotificationFacade,
	private val agentMetrics: AgentMetrics,
	private val telegramMessenger: ObjectProvider<TelegramMessenger>,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun listExercises(): String {
		val exercises = workoutBotFacade.listExercises()
		if (exercises.isEmpty()) {
			return "Упражнений пока нет."
		}
		return exercises.joinToString("\n") { ex ->
			"• ${ex.name} (${ex.muscleGroup}) [id=${ex.id}]"
		}
	}

	fun createExercise(
		name: String,
		muscleGroup: String?,
	): String = workoutBotFacade.createExercise(name, muscleGroup)

	fun renameExercise(
		currentName: String,
		newName: String,
		muscleGroup: String?,
	): String = workoutBotFacade.renameExercise(currentName, newName, muscleGroup)

	fun logWorkout(
		exerciseName: String,
		performedOn: String?,
		weightKg: Int,
		setCount: Int,
		repsPerSet: Int,
		maxReps: Int,
	): String {
		val date =
			when (val resolved = resolvePerformedOn(performedOn)) {
				is DateResolve.Ok -> resolved.date
				is DateResolve.Invalid -> return performedOnError(performedOn)
			}
		return workoutBotFacade.logWorkout(
			exerciseName = exerciseName,
			performedOn = date,
			weightKg = weightKg,
			setCount = setCount,
			repsPerSet = repsPerSet,
			maxReps = maxReps,
		)
	}

	fun deleteWorkout(
		exerciseName: String,
		performedOn: String?,
	): String {
		val date =
			when (val resolved = resolvePerformedOn(performedOn)) {
				is DateResolve.Ok -> resolved.date
				is DateResolve.Invalid -> return performedOnError(performedOn)
			}
		return workoutBotFacade.deleteWorkout(exerciseName, date)
	}

	fun getExerciseProgresses(
		exercises: String?,
		recentSessions: Int?,
	): String {
		val names =
			when (val resolved = resolveExerciseList(exercises)) {
				is ExerciseListResolve.Ok -> resolved.names
				is ExerciseListResolve.Error -> return ToolExecutionFeedback.failure(resolved.message)
			}
		return workoutBotFacade.getExerciseProgressSummaries(names, recentSessions ?: 6)
	}

	fun getDaySummaries(
		from: String?,
		to: String?,
		days: String?,
	): String {
		val dates =
			when (val resolved = resolveDayList(from, to, days)) {
				is DayListResolve.Ok -> resolved.dates
				is DayListResolve.Error ->
					return ToolExecutionFeedback.failure(
						error = resolved.message,
						hint = "Исправь даты и вызови инструмент снова. Формат YYYY-MM-DD, значения в кавычках в arguments JSON.",
					)
			}
		return workoutBotFacade.getDaySummaries(dates)
	}

	fun sendNotification(
		chatId: Long,
		message: String,
	): String = temporalNotificationFacade.sendNow(chatId, message)

	fun scheduleNotification(
		chatId: Long,
		message: String,
		deliverAt: String,
	): String = temporalNotificationFacade.schedule(chatId, message, deliverAt)

	fun cancelNotification(workflowId: String): String = temporalNotificationFacade.cancel(workflowId)

	fun sendRichMessage(
		chatId: Long,
		text: String,
		buttons: String?,
	): String {
		val messenger =
			telegramMessenger.getIfAvailable()
				?: return ToolExecutionFeedback.failure("Telegram недоступен.")
		val markup =
			try {
				TelegramButtonParser.parse(buttons)
			} catch (ex: IllegalArgumentException) {
				return ToolExecutionFeedback.failure(ex.message ?: "Неверный формат buttons")
			}
		messenger.sendHtmlMessage(chatId, text, markup)
		return if (markup == null) {
			"Сообщение отправлено."
		} else {
			"Сообщение с кнопками отправлено."
		}
	}

	fun runTool(
		name: String,
		chatId: Long,
		args: Map<String, String?>,
	): String {
		val toolName = camelToSnake(name)
		val toolArgs = args.normalizeKeys()
		log.info("Tool {} chatId={} args={}", toolName, chatId, LogPreview.of(toolArgs.toString(), max = 240))
		return agentMetrics.timeTool(toolName, "temporal") {
			runToolBody(toolName, chatId, toolArgs, rawName = name)
		}
	}

	private fun runToolBody(
		toolName: String,
		chatId: Long,
		toolArgs: Map<String, String?>,
		rawName: String,
	): String {
		val result =
			try {
				when (toolName) {
					"list_exercises" -> listExercises()
					"create_exercise" ->
						createExercise(
							toolArgs.require("name"),
							toolArgs.optional("muscle_group"),
						)
					"rename_exercise" ->
						renameExercise(
							toolArgs.require("current_name"),
							toolArgs.require("new_name"),
							toolArgs.optional("muscle_group"),
						)
					"log_workout" ->
						logWorkout(
							exerciseName = toolArgs.require("exercise_name"),
							performedOn = toolArgs.optional("performed_on"),
							weightKg = toolArgs.requireInt("weight_kg"),
							setCount = toolArgs.optionalInt("set_count") ?: 3,
							repsPerSet = toolArgs.requireInt("reps_per_set"),
							maxReps = toolArgs.requireInt("max_reps"),
						)
					"delete_workout" ->
						deleteWorkout(
							toolArgs.require("exercise_name"),
							toolArgs.optional("performed_on"),
						)
					"get_exercise_progresses" ->
						getExerciseProgresses(
							toolArgs.require("exercises"),
							toolArgs.optionalInt("recent_sessions"),
						)
					"get_day_summaries" ->
						getDaySummaries(
							toolArgs.optional("from"),
							toolArgs.optional("to"),
							toolArgs.optional("days"),
						)
					"send_notification" -> sendNotification(chatId, toolArgs.require("message"))
					"schedule_notification" ->
						scheduleNotification(
							chatId,
							toolArgs.require("message"),
							toolArgs.require("deliver_at"),
						)
					"cancel_notification" -> cancelNotification(toolArgs.require("workflow_id"))
					"send_rich_message" ->
						sendRichMessage(
							chatId = chatId,
							text = toolArgs.require("text"),
							buttons = toolArgs.optional("buttons"),
						)
					else ->
						ToolExecutionFeedback.failure("Неизвестный инструмент: $rawName")
				}
			} catch (ex: ResponseStatusException) {
				ToolExecutionFeedback.failure(ex.reason ?: ex.message ?: "Request failed")
			} catch (ex: IllegalArgumentException) {
				ToolExecutionFeedback.failure(ex.message ?: "Invalid tool arguments")
			} catch (ex: Exception) {
				ToolExecutionFeedback.failure(ex.message ?: "Tool execution failed")
			}
		log.info("Tool {} result={}", toolName, LogPreview.of(result, max = 240))
		return result
	}

	fun trackedDirectTool(
		toolName: String,
		block: () -> String,
	): String = agentMetrics.timeTool(toolName, "direct", block)

	private fun Map<String, String?>.normalizeKeys(): Map<String, String?> =
		mapKeys { (key, _) -> camelToSnake(key) }

	private fun camelToSnake(value: String): String =
		value
			.replace(Regex("([a-z0-9])([A-Z])")) { "${it.groupValues[1]}_${it.groupValues[2]}" }
			.lowercase()

	private sealed interface DateResolve {
		data class Ok(
			val date: LocalDate?,
		) : DateResolve

		data object Invalid : DateResolve
	}

	private fun resolvePerformedOn(raw: String?): DateResolve {
		if (raw.isNullOrBlank()) {
			return DateResolve.Ok(null)
		}
		return try {
			DateResolve.Ok(LocalDate.parse(raw.trim()))
		} catch (_: DateTimeParseException) {
			DateResolve.Invalid
		}
	}

	private fun performedOnError(raw: String?): String =
		ToolExecutionFeedback.failure("Неверная дата performed_on: ${raw ?: "формат YYYY-MM-DD"}")

	internal sealed interface DayListResolve {
		data class Ok(
			val dates: List<LocalDate>,
		) : DayListResolve

		data class Error(
			val message: String,
		) : DayListResolve
	}

	internal fun resolveDayList(
		from: String?,
		to: String?,
		days: String?,
	): DayListResolve {
		val daysList = days?.trim()?.takeIf { it.isNotEmpty() }
		if (daysList != null) {
			val parsed =
				daysList.split(",").map { part ->
					try {
						LocalDate.parse(part.trim())
					} catch (_: DateTimeParseException) {
						return DayListResolve.Error("Неверная дата в days: $part (формат YYYY-MM-DD)")
					}
				}
			if (parsed.distinct().size > MAX_DAY_SUMMARIES) {
				return DayListResolve.Error("Слишком много дней в days (макс. $MAX_DAY_SUMMARIES).")
			}
			return DayListResolve.Ok(parsed)
		}
		val fromRaw = from?.trim()?.takeIf { it.isNotEmpty() }
		val toRaw = to?.trim()?.takeIf { it.isNotEmpty() }
		if (fromRaw == null && toRaw == null) {
			return DayListResolve.Error("Укажи from+to (интервал) или days (даты через запятую, YYYY-MM-DD).")
		}
		if (fromRaw == null || toRaw == null) {
			return DayListResolve.Error("Для интервала нужны оба поля from и to (YYYY-MM-DD).")
		}
		val fromDate =
			try {
				LocalDate.parse(fromRaw)
			} catch (_: DateTimeParseException) {
				return DayListResolve.Error("Неверная дата from: $fromRaw")
			}
		val toDate =
			try {
				LocalDate.parse(toRaw)
			} catch (_: DateTimeParseException) {
				return DayListResolve.Error("Неверная дата to: $toRaw")
			}
		if (fromDate.isAfter(toDate)) {
			return DayListResolve.Error("from не может быть позже to.")
		}
		val span = ChronoUnit.DAYS.between(fromDate, toDate) + 1
		if (span > MAX_DAY_SUMMARIES) {
			return DayListResolve.Error("Интервал слишком большой (макс. $MAX_DAY_SUMMARIES дней).")
		}
		val range = generateSequence(fromDate) { d -> if (d.isBefore(toDate)) d.plusDays(1) else null }.toList()
		return DayListResolve.Ok(range)
	}

	internal sealed interface ExerciseListResolve {
		data class Ok(
			val names: List<String>,
		) : ExerciseListResolve

		data class Error(
			val message: String,
		) : ExerciseListResolve
	}

	internal fun resolveExerciseList(exercises: String?): ExerciseListResolve {
		val raw = exercises?.trim()?.takeIf { it.isNotEmpty() }
			?: return ExerciseListResolve.Error("Укажи exercises (названия упражнений через запятую).")
		val names =
			raw.split(",").map { it.trim() }.filter { it.isNotEmpty() }
		if (names.isEmpty()) {
			return ExerciseListResolve.Error("Укажи exercises (названия упражнений через запятую).")
		}
		if (names.size > MAX_EXERCISE_PROGRESS) {
			return ExerciseListResolve.Error("Слишком много упражнений (макс. $MAX_EXERCISE_PROGRESS).")
		}
		return ExerciseListResolve.Ok(names)
	}

	private fun Map<String, String?>.require(key: String): String =
		this[key]?.trim()?.takeIf { it.isNotEmpty() }
			?: throw IllegalArgumentException("Нужно поле $key")

	private fun Map<String, String?>.optional(key: String): String? =
		this[key]?.trim()?.takeIf { it.isNotEmpty() }

	private fun Map<String, String?>.requireInt(key: String): Int =
		optional(key)?.toIntOrNull()
			?: throw IllegalArgumentException("Нужно поле $key")

	private fun Map<String, String?>.optionalInt(key: String): Int? = optional(key)?.toIntOrNull()
}
