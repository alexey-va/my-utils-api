package agent

import "github.com/alexey-va/my-utils-api/internal/openrouter"

func ToolDefinitions(temporalEnabled bool) []openrouter.Tool {
	type property = map[string]any
	definition := func(name, description string, properties property, required ...string) openrouter.Tool {
		parameters := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			parameters["required"] = required
		}
		return openrouter.Tool{Type: "function", Function: openrouter.ToolFunction{Name: name, Description: description, Parameters: parameters}}
	}
	text := func(description string) property { return property{"type": "string", "description": description} }
	integer := func(description string) property { return property{"type": "integer", "description": description} }
	tools := []openrouter.Tool{
		definition("list_exercises", "Список упражнений в дневнике.", property{}),
		definition("create_exercise", "Создать упражнение, если его ещё нет.", property{"name": text("Название"), "muscle_group": text("Группа мышц")}, "name"),
		definition("rename_exercise", "Переименовать упражнение.", property{"current_name": text("Текущее название"), "new_name": text("Новое название"), "muscle_group": text("Группа мышц")}, "current_name", "new_name"),
		definition("delete_workout", "Удалить запись за день.", property{"exercise_name": text("Упражнение"), "performed_on": text("Дата YYYY-MM-DD")}, "exercise_name"),
		definition("log_workout", "Записать тренировку. notation: 70 3*10/12, 70 10/12, 70 10/10, 70 7/7/7 или 70/75/80 10/10/10.", property{"exercise_name": text("Упражнение"), "notation": text("Запись подходов"), "date": text("Дата YYYY-MM-DD")}, "exercise_name", "notation"),
		definition("get_progress", "Прогресс по одному упражнению за последние сессии.", property{"exercise": text("Упражнение"), "recent_sessions": integer("Количество сессий")}, "exercise"),
		definition("get_days", "Записи за дни. days — даты YYYY-MM-DD через запятую. Без days — сегодня.", property{"days": text("Даты через запятую")}),
		definition("log_body_weight", "Записать вес тела, не вес на штанге.", property{"weight_kg": property{"type": "number"}, "date": text("Дата YYYY-MM-DD")}, "weight_kg"),
		definition("get_body_weight", "История веса тела за последние дни.", property{"recent_days": integer("Количество дней")}),
		definition("remember_fact", "Запомнить долгосрочный факт о пользователе.", property{"content": text("Факт")}, "content"),
		definition("forget_fact", "Забыть факт по fact_id.", property{"fact_id": text("ID факта")}, "fact_id"),
		definition("send_rich_message", "Сообщение с кнопками.", property{"text": text("Текст"), "buttons": text("Кнопки: Текст:callback,Ещё:callback;Ниже:callback")}, "text"),
		definition("send_progress_chart", "PNG-график прогресса и отправка в чат.", property{"exercise_name": text("Упражнение"), "recent_sessions": integer("Количество сессий")}, "exercise_name"),
		definition("estimate_1rm", "Оценка одноповторного максимума по последней или указанной сессии.", property{"exercise_name": text("Упражнение"), "date": text("Дата YYYY-MM-DD")}, "exercise_name"),
	}
	if temporalEnabled {
		tools = append(tools,
			definition("send_notification", "Сразу отправить сообщение в чат.", property{"message": text("Сообщение")}, "message"),
			definition("schedule_notification", "Напоминание на время deliver_at в ISO datetime.", property{"message": text("Сообщение"), "deliver_at": text("ISO datetime")}, "message", "deliver_at"),
			definition("cancel_notification", "Отменить напоминание по workflow_id.", property{"workflow_id": text("Workflow ID")}, "workflow_id"),
		)
	}
	return tools
}

func isImmediateReturn(name string) bool {
	switch NormalizeToolName(name) {
	case "send_rich_message", "send_progress_chart", "estimate_1rm":
		return true
	default:
		return false
	}
}
