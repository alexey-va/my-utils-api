package dev.myutils.api.agent.langchain

import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.langchain4j.agent.tool.Tool

/**
 * LangChain4j tools for one chat. Instance is created per agent turn with bound [chatId].
 */
class WorkoutLangChainTools(
	private val chatId: Long,
	private val toolsService: WorkoutToolsService,
	private val temporalEnabled: Boolean,
) {
	@Tool("Список всех упражнений в дневнике (id, название, группа мышц).")
	fun listExercises(): String = tracked("list_exercises") { toolsService.listExercises() }

	@Tool("Переименовать упражнение в дневнике (записи сохраняются). Можно сменить группу мышц.")
	fun renameExercise(
		currentName: String,
		newName: String,
		muscleGroup: String? = null,
	): String = tracked("rename_exercise") { toolsService.renameExercise(currentName, newName, muscleGroup) }

	@Tool("Создать новое упражнение, если его ещё нет. Для гантелей укажи «гантели» в названии.")
	fun createExercise(
		name: String,
		muscleGroup: String? = null,
	): String = tracked("create_exercise") { toolsService.createExercise(name, muscleGroup) }

	@Tool("Удалить запись за день (упражнение + дата).")
	fun deleteWorkout(
		exerciseName: String,
		performedOn: String? = null,
	): String = tracked("delete_workout") { toolsService.deleteWorkout(exerciseName, performedOn) }

	@Tool(
		"Записать/обновить за день: вес 3*X/МАХ → set_count=3, reps_per_set=X, max_reps=МАХ, weight_kg полный.",
	)
	fun logWorkout(
		exerciseName: String,
		weightKg: Int,
		repsPerSet: Int,
		maxReps: Int,
		performedOn: String? = null,
		setCount: Int = 3,
	): String =
		tracked("log_workout") {
			toolsService.logWorkout(
				exerciseName = exerciseName,
				performedOn = performedOn,
				weightKg = weightKg,
				setCount = setCount,
				repsPerSet = repsPerSet,
				maxReps = maxReps,
			)
		}

	@Tool("Прогресс по упражнениям. exercises — названия через запятую (одно тоже списком). Макс. 15.")
	fun getExerciseProgresses(
		exercises: String,
		recentSessions: Int? = 6,
	): String =
		tracked("get_exercise_progresses") {
			toolsService.getExerciseProgresses(exercises, recentSessions)
		}

	@Tool("Записи за дни. days — даты YYYY-MM-DD через запятую, или from+to — интервал включительно. Макс. 31 день.")
	fun getDaySummaries(
		from: String? = null,
		to: String? = null,
		days: String? = null,
	): String = tracked("get_day_summaries") { toolsService.getDaySummaries(from, to, days) }

	@Tool("Сразу отправить сообщение пользователю в этот Telegram-чат (через Temporal).")
	fun sendNotification(message: String): String {
		requireTemporal()
		return tracked("send_notification") { toolsService.sendNotification(chatId, message) }
	}

	@Tool("Запланировать напоминание в Telegram на время deliver_at (Temporal workflow).")
	fun scheduleNotification(
		message: String,
		deliverAt: String,
	): String {
		requireTemporal()
		return tracked("schedule_notification") {
			toolsService.scheduleNotification(chatId, message, deliverAt)
		}
	}

	@Tool("Отменить запланированное уведомление по workflow_id из ответа schedule_notification.")
	fun cancelNotification(workflowId: String): String {
		requireTemporal()
		return tracked("cancel_notification") { toolsService.cancelNotification(workflowId) }
	}

	private fun tracked(
		toolName: String,
		block: () -> String,
	): String = toolsService.trackedDirectTool(toolName, block)

	private fun requireTemporal() {
		check(temporalEnabled) { "Temporal выключен — уведомления недоступны." }
	}

	companion object {
		fun create(
			chatId: Long,
			toolsService: WorkoutToolsService,
			properties: MyUtilsProperties,
		): WorkoutLangChainTools =
			WorkoutLangChainTools(
				chatId = chatId,
				toolsService = toolsService,
				temporalEnabled = properties.temporal.enabled,
			)
	}
}
