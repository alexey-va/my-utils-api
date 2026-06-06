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
	fun listExercises(): String = toolsService.listExercises()

	@Tool("Переименовать упражнение в дневнике (записи сохраняются). Можно сменить группу мышц.")
	fun renameExercise(
		currentName: String,
		newName: String,
		muscleGroup: String? = null,
	): String = toolsService.renameExercise(currentName, newName, muscleGroup)

	@Tool("Создать новое упражнение, если его ещё нет. Для гантелей укажи «гантели» в названии.")
	fun createExercise(
		name: String,
		muscleGroup: String? = null,
	): String = toolsService.createExercise(name, muscleGroup)

	@Tool("Удалить запись за день (упражнение + дата).")
	fun deleteWorkout(
		exerciseName: String,
		performedOn: String? = null,
	): String = toolsService.deleteWorkout(exerciseName, performedOn)

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
		toolsService.logWorkout(
			exerciseName = exerciseName,
			performedOn = performedOn,
			weightKg = weightKg,
			setCount = setCount,
			repsPerSet = repsPerSet,
			maxReps = maxReps,
		)

	@Tool("Прогресс по упражнению (если нет в снимке).")
	fun getExerciseProgress(
		exerciseName: String,
		recentSessions: Int? = 6,
	): String = toolsService.getExerciseProgress(exerciseName, recentSessions)

	@Tool("Записи за день (если нет в снимке).")
	fun getDaySummary(performedOn: String? = null): String = toolsService.getDaySummary(performedOn)

	@Tool("Сразу отправить сообщение пользователю в этот Telegram-чат (через Temporal).")
	fun sendNotification(message: String): String {
		requireTemporal()
		return toolsService.sendNotification(chatId, message)
	}

	@Tool("Запланировать напоминание в Telegram на время deliver_at (Temporal workflow).")
	fun scheduleNotification(
		message: String,
		deliverAt: String,
	): String {
		requireTemporal()
		return toolsService.scheduleNotification(chatId, message, deliverAt)
	}

	@Tool("Отменить запланированное уведомление по workflow_id из ответа schedule_notification.")
	fun cancelNotification(workflowId: String): String {
		requireTemporal()
		return toolsService.cancelNotification(workflowId)
	}

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
