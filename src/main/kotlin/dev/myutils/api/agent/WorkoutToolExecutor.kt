package dev.myutils.api.agent

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.openrouter.ToolCall
import dev.myutils.api.service.WorkoutBotFacade
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import org.springframework.web.server.ResponseStatusException
import java.time.LocalDate
import java.time.format.DateTimeParseException

@Component
class WorkoutToolExecutor(
	private val workoutBotFacade: WorkoutBotFacade,
	private val objectMapper: ObjectMapper,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun execute(call: ToolCall): String {
		val args = parseArgs(call.function.arguments)
		log.info("Tool {} args={}", call.function.name, LogPreview.of(call.function.arguments, max = 240))
		val result =
			try {
				when (call.function.name) {
					"list_exercises" -> listExercises()
					"create_exercise" -> createExercise(args)
					"rename_exercise" -> renameExercise(args)
					"log_workout" -> logWorkout(args)
					"delete_workout" -> deleteWorkout(args)
					"get_exercise_progress" -> getExerciseProgress(args)
					"get_day_summary" -> getDaySummary(args)
					else -> "Неизвестный инструмент: ${call.function.name}"
				}
			} catch (ex: ResponseStatusException) {
				ex.reason ?: ex.message ?: "Request failed"
			} catch (ex: Exception) {
				ex.message ?: "Tool execution failed"
			}
		log.info("Tool {} result={}", call.function.name, LogPreview.of(result, max = 240))
		return result
	}

	private fun listExercises(): String {
		val exercises = workoutBotFacade.listExercises()
		if (exercises.isEmpty()) {
			return "Упражнений пока нет."
		}
		return exercises.joinToString("\n") { ex ->
			"• ${ex.name} (${ex.muscleGroup}) [id=${ex.id}]"
		}
	}

	private fun createExercise(args: JsonNode): String {
		val name = text(args, "name") ?: return "Нужно поле name"
		val muscleGroup = text(args, "muscle_group")
		return workoutBotFacade.createExercise(name, muscleGroup)
	}

	private fun renameExercise(args: JsonNode): String {
		val currentName = text(args, "current_name") ?: return "Нужно поле current_name"
		val newName = text(args, "new_name") ?: return "Нужно поле new_name"
		val muscleGroup = text(args, "muscle_group")
		return workoutBotFacade.renameExercise(currentName, newName, muscleGroup)
	}

	private fun logWorkout(args: JsonNode): String {
		val exerciseName = text(args, "exercise_name") ?: return "Нужно поле exercise_name"
		val weightKg = int(args, "weight_kg") ?: return "Нужно поле weight_kg"
		val setCount = int(args, "set_count") ?: 3
		val repsPerSet = int(args, "reps_per_set") ?: return "Нужно поле reps_per_set"
		val maxReps = int(args, "max_reps") ?: return "Нужно поле max_reps"
		val performedOn =
			text(args, "performed_on")?.let { raw ->
				try {
					LocalDate.parse(raw)
				} catch (_: DateTimeParseException) {
					return "Неверная дата performed_on: формат YYYY-MM-DD"
				}
			}
		return workoutBotFacade.logWorkout(
			exerciseName = exerciseName,
			performedOn = performedOn,
			weightKg = weightKg,
			setCount = setCount,
			repsPerSet = repsPerSet,
			maxReps = maxReps,
		)
	}

	private fun deleteWorkout(args: JsonNode): String {
		val exerciseName = text(args, "exercise_name") ?: return "Нужно поле exercise_name"
		val performedOn =
			text(args, "performed_on")?.let { raw ->
				try {
					LocalDate.parse(raw)
				} catch (_: DateTimeParseException) {
					return "Неверная дата performed_on: формат YYYY-MM-DD"
				}
			}
		return workoutBotFacade.deleteWorkout(exerciseName, performedOn)
	}

	private fun getExerciseProgress(args: JsonNode): String {
		val exerciseName = text(args, "exercise_name") ?: return "Нужно поле exercise_name"
		val recent = int(args, "recent_sessions") ?: 6
		return workoutBotFacade.getExerciseProgressSummary(exerciseName, recent)
	}

	private fun getDaySummary(args: JsonNode): String {
		val performedOn =
			text(args, "performed_on")?.let { raw ->
				try {
					LocalDate.parse(raw)
				} catch (_: DateTimeParseException) {
					return "Неверная дата performed_on: формат YYYY-MM-DD"
				}
			}
		return workoutBotFacade.getDaySummary(performedOn)
	}

	private fun parseArgs(raw: String): JsonNode =
		if (raw.isBlank()) {
			objectMapper.createObjectNode()
		} else {
			objectMapper.readTree(raw)
		}

	private fun text(
		node: JsonNode,
		field: String,
	): String? = node.get(field)?.takeUnless { it.isNull }?.asText()?.trim()?.takeIf { it.isNotEmpty() }

	private fun int(
		node: JsonNode,
		field: String,
	): Int? = node.get(field)?.takeUnless { it.isNull }?.asInt()
}
