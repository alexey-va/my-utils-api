package agent

import (
	"context"
	"fmt"
)

type TurnStatus interface {
	Thinking(context.Context, int64, int)
	ToolsStarted(context.Context, int64, []string)
	ToolRunning(context.Context, int64, string)
	ComposingReply(context.Context, int64)
	Complete(context.Context, int64)
}

type turnStatusContextKey struct{}

func WithTurnStatus(ctx context.Context, status TurnStatus) context.Context {
	if status == nil {
		return ctx
	}
	return context.WithValue(ctx, turnStatusContextKey{}, status)
}

func statusFromContext(ctx context.Context) TurnStatus {
	status, _ := ctx.Value(turnStatusContextKey{}).(TurnStatus)
	return status
}

func ToolStatusLabel(rawName string) string {
	name := NormalizeToolName(rawName)
	if label, ok := map[string]string{
		"list_exercises":          "Загружаю список упражнений…",
		"create_exercise":         "Создаю упражнение…",
		"rename_exercise":         "Переименовываю упражнение…",
		"log_workout":             "Записываю в дневник…",
		"delete_workout":          "Удаляю запись…",
		"get_progress":            "Получаю прогресс…",
		"get_exercise_progresses": "Получаю прогресс…",
		"get_days":                "Получаю статистику по дням…",
		"get_day_summaries":       "Получаю статистику по дням…",
		"log_body_weight":         "Записываю вес тела…",
		"get_body_weight":         "Смотрю вес тела…",
		"remember_fact":           "Запоминаю факт…",
		"forget_fact":             "Удаляю из памяти…",
		"manage_user_fact":        "Обновляю память…",
		"send_rich_message":       "Отправляю сообщение с кнопками…",
		"send_progress_chart":     "Строю график прогресса…",
		"estimate_1rm":            "Считаю 1ПМ…",
		"send_notification":       "Отправляю уведомление…",
		"schedule_notification":   "Планирую напоминание…",
		"cancel_notification":     "Отменяю напоминание…",
	}[name]; ok {
		return label
	}
	return "Обрабатываю…"
}

func ToolsStatusLabel(rawNames []string) string {
	distinct := map[string]bool{}
	for _, name := range rawNames {
		distinct[NormalizeToolName(name)] = true
	}
	if len(distinct) == 0 {
		return "Обрабатываю…"
	}
	if len(distinct) == 1 {
		return ToolStatusLabel(rawNames[0])
	}
	return fmt.Sprintf("Выполняю %d действия…", len(distinct))
}
