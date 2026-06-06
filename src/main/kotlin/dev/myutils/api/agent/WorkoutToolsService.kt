package dev.myutils.api.agent

import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.temporal.TemporalNotificationFacade
import dev.myutils.api.infra.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Service
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.time.format.DateTimeParseException

@Service
class WorkoutToolsService(
	private val workoutBotFacade: WorkoutBotFacade,
	private val temporalNotificationFacade: TemporalNotificationFacade,
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

	fun getExerciseProgress(
		exerciseName: String,
		recentSessions: Int?,
	): String = workoutBotFacade.getExerciseProgressSummary(exerciseName, recentSessions ?: 6)

	fun getDaySummary(performedOn: String?): String {
		val date =
			when (val resolved = resolvePerformedOn(performedOn)) {
				is DateResolve.Ok -> resolved.date
				is DateResolve.Invalid -> return performedOnError(performedOn)
			}
		return workoutBotFacade.getDaySummary(date)
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

	fun runTool(
		name: String,
		chatId: Long,
		args: Map<String, String?>,
	): String {
		val toolName = camelToSnake(name)
		val toolArgs = args.normalizeKeys()
		log.info("Tool {} chatId={} args={}", toolName, chatId, LogPreview.of(toolArgs.toString(), max = 240))
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
					"get_exercise_progress" ->
						getExerciseProgress(
							toolArgs.require("exercise_name"),
							toolArgs.optionalInt("recent_sessions"),
						)
					"get_day_summary" -> getDaySummary(toolArgs.optional("performed_on"))
					"send_notification" -> sendNotification(chatId, toolArgs.require("message"))
					"schedule_notification" ->
						scheduleNotification(
							chatId,
							toolArgs.require("message"),
							toolArgs.require("deliver_at"),
						)
					"cancel_notification" -> cancelNotification(toolArgs.require("workflow_id"))
					else -> "Неизвестный инструмент: $name"
				}
			} catch (ex: ResponseStatusException) {
				ex.reason ?: ex.message ?: "Request failed"
			} catch (ex: IllegalArgumentException) {
				ex.message ?: "Invalid tool arguments"
			} catch (ex: Exception) {
				ex.message ?: "Tool execution failed"
			}
		log.info("Tool {} result={}", toolName, LogPreview.of(result, max = 240))
		return result
	}

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

	private fun performedOnError(raw: String?): String = "Неверная дата performed_on: ${raw ?: "формат YYYY-MM-DD"}"

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
