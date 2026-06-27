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
		"Записать/обновить за день. Классика: 3*X/МАХ → set_count=3, reps_per_set=X, max_reps=МАХ. " +
			"Разные повторы: set_reps=\"10/10/9/9\" или \"10,10,9,12\" (тогда reps_per_set и max_reps не нужны).",
	)
	fun logWorkout(
		exerciseName: String,
		weightKg: Int,
		repsPerSet: Int? = null,
		maxReps: Int? = null,
		performedOn: String? = null,
		setCount: Int = 3,
		setReps: String? = null,
	): String =
		tracked("log_workout") {
			toolsService.logWorkout(
				exerciseName = exerciseName,
				performedOn = performedOn,
				weightKg = weightKg,
				setCount = setCount,
				repsPerSet = repsPerSet,
				maxReps = maxReps,
				setRepsRaw = setReps,
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

	@Tool(
		"Отправить HTML в Telegram с inline-кнопками (returnDirect: сообщение уже в чате, не дублируй текст). " +
			"buttons: ряды через ';', кнопки через ',', формат подпись:callback. " +
			"Пример: Сегодня:что на сегодня,Неделя:план;Отмена:отмена",
	)
	fun sendRichMessage(
		text: String,
		buttons: String? = null,
	): String = tracked("send_rich_message") { toolsService.sendRichMessage(chatId, text, buttons) }

	@Tool("Сразу отправить сообщение пользователю в этот Telegram-чат (через Temporal).")
	fun sendNotification(message: String): String {
		requireTemporal()
		return tracked("send_notification") { toolsService.sendNotification(chatId, message) }
	}

	@Tool(
		"Память о пользователе (цели, травмы, предпочтения). action: remember — новый факт (content); " +
			"update — правка по fact_id (content); forget — удалить по fact_id. " +
			"confidence — уверенность 0..1 (например 0.9 явное, 0.6 гипотеза). " +
			"fact_id бери из блока «Известные факты» в system-контексте. " +
			"Не запоминай разовые тренировки — они уже в дневнике.",
	)
	fun manageUserFact(
		action: String,
		content: String? = null,
		factId: String? = null,
		confidence: Double? = null,
	): String =
		tracked("manage_user_fact") {
			toolsService.manageUserFact(chatId, action, content, factId, confidence)
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
