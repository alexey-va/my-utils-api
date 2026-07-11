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
	@Tool("Список упражнений в дневнике.")
	fun listExercises(): String = tracked("list_exercises") { toolsService.listExercises() }

	@Tool("Создать упражнение, если его ещё нет.")
	fun createExercise(
		name: String,
		muscleGroup: String? = null,
	): String = tracked("create_exercise") { toolsService.createExercise(name, muscleGroup) }

	@Tool("Переименовать упражнение.")
	fun renameExercise(
		currentName: String,
		newName: String,
		muscleGroup: String? = null,
	): String = tracked("rename_exercise") { toolsService.renameExercise(currentName, newName, muscleGroup) }

	@Tool("Удалить запись за день.")
	fun deleteWorkout(
		exerciseName: String,
		performedOn: String? = null,
	): String = tracked("delete_workout") { toolsService.deleteWorkout(exerciseName, performedOn) }

	@Tool(
		"Записать тренировку. notation — как в дневнике: " +
			"«70 3*10/12» (3 рабочих+МАХ), «70 8/12» (2 подхода), «70 7/7/7», «70/75/80 10/10/10» (разные веса). " +
			"date — YYYY-MM-DD, по умолчанию сегодня.",
	)
	fun logWorkout(
		exerciseName: String,
		notation: String,
		date: String? = null,
	): String =
		tracked("log_workout") {
			toolsService.logWorkout(
				exerciseName = exerciseName,
				performedOn = date,
				notation = notation,
			)
		}

	@Tool("Прогресс по одному упражнению за последние сессии.")
	fun getProgress(
		exercise: String,
		recentSessions: Int? = 6,
	): String =
		tracked("get_exercise_progresses") {
			toolsService.getExerciseProgresses(exercise, recentSessions)
		}

	@Tool("Записи за дни. days — даты YYYY-MM-DD через запятую. Без days — сегодня.")
	fun getDays(days: String? = null): String = tracked("get_day_summaries") { toolsService.getDaySummaries(days) }

	@Tool("Запомнить факт о пользователе (цель, травма, предпочтение). Не для разовых тренировок.")
	fun rememberFact(content: String): String =
		tracked("remember_fact") {
			toolsService.manageUserFact(chatId, "remember", content, null, null)
		}

	@Tool("Забыть факт по fact_id из блока «Известные факты».")
	fun forgetFact(factId: String): String =
		tracked("forget_fact") {
			toolsService.manageUserFact(chatId, "forget", null, factId, null)
		}

	@Tool("Сообщение с кнопками. buttons: «Текст:callback,Ещё:callback» (ряды через ;).")
	fun sendRichMessage(
		text: String,
		buttons: String? = null,
	): String = tracked("send_rich_message") { toolsService.sendRichMessage(chatId, text, buttons) }

	@Tool(
		"PNG-график прогресса (вес + МАХ повторы) и отправка в чат. " +
			"Когда просят график, динамику, визуализацию прогресса.",
	)
	fun sendProgressChart(
		exerciseName: String,
		recentSessions: Int? = 12,
	): String =
		tracked("send_progress_chart") {
			toolsService.sendProgressChart(chatId, exerciseName, recentSessions)
		}

	@Tool("Сразу отправить сообщение в чат.")
	fun sendNotification(message: String): String {
		requireTemporal()
		return tracked("send_notification") { toolsService.sendNotification(chatId, message) }
	}

	@Tool("Напоминание в чат на время deliverAt (ISO datetime).")
	fun scheduleNotification(
		message: String,
		deliverAt: String,
	): String {
		requireTemporal()
		return tracked("schedule_notification") {
			toolsService.scheduleNotification(chatId, message, deliverAt)
		}
	}

	@Tool("Отменить напоминание по workflow_id.")
	fun cancelNotification(workflowId: String): String {
		requireTemporal()
		return tracked("cancel_notification") { toolsService.cancelNotification(workflowId) }
	}

	private fun tracked(
		toolName: String,
		block: () -> String,
	): String = toolsService.trackedDirectTool(chatId, toolName, block)

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
