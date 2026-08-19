package temporal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alexey-va/my-utils-api/internal/agent"
	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
)

type Messenger interface {
	SendHTMLMessage(context.Context, int64, string, string) (int, error)
	SendPhoto(context.Context, int64, []byte, string) error
}

type WeeklyReportRenderer interface {
	RenderSteps([]health.StepDay, time.Time, time.Time) ([]byte, error)
	RenderWeight([]health.WeightDay, time.Time, time.Time) ([]byte, error)
}

type Activities struct {
	Workout   *workout.Service
	Health    *health.Service
	Messenger Messenger
	Renderer  WeeklyReportRenderer
	Agent     agent.Turner
	Status    agent.TurnStatus
	Metrics   agent.MetricsRecorder
	Allowed   map[int64]bool
}

func (a *Activities) SendTelegramMessage(ctx context.Context, chatID int64, message string) error {
	if a.Messenger == nil {
		return errors.New("Telegram messenger is not configured")
	}
	_, err := a.Messenger.SendHTMLMessage(ctx, chatID, message, "")
	return err
}

func (a *Activities) HasWorkoutToday(ctx context.Context, zoneID string) (bool, error) {
	location, err := time.LoadLocation(zoneID)
	if err != nil {
		return false, err
	}
	grid, err := a.Workout.Grid(ctx)
	if err != nil {
		return false, err
	}
	today := time.Now().In(location).Format(time.DateOnly)
	for _, row := range grid.Rows {
		if _, exists := row.Cells[today]; exists {
			return true, nil
		}
	}
	return false, nil
}

func (a *Activities) SendEveningReminder(ctx context.Context, chatID int64) error {
	return a.SendTelegramMessage(ctx, chatID, "Сегодня в дневнике пусто. Запиши тренировку или напиши «что на сегодня» — составлю план.")
}

func (a *Activities) SendWeeklyReport(ctx context.Context, input WeeklyReportActivityInput) error {
	if a.Messenger == nil || a.Renderer == nil {
		return errors.New("weekly report delivery is not configured")
	}
	reportDate, err := time.Parse(time.DateOnly, input.ReportDate)
	if err != nil {
		return err
	}
	lookback := min(max(input.LookbackDays, 7), 366)
	from := reportDate.AddDate(0, 0, -(lookback - 1))
	steps, err := a.Health.StepsHistory(ctx, lookback, reportDate)
	if err != nil {
		return err
	}
	weights, err := a.Health.WeightHistory(ctx, lookback, reportDate)
	if err != nil {
		return err
	}
	stepsPNG, err := a.Renderer.RenderSteps(steps.Days, from, reportDate)
	if err != nil {
		return err
	}
	weightPNG, err := a.Renderer.RenderWeight(weights.Days, from, reportDate)
	if err != nil {
		return err
	}
	captionDate := reportDate.Format("02.01.2006")
	if err := a.Messenger.SendPhoto(ctx, input.ChatID, stepsPNG, fmt.Sprintf("<b>Шаги · еженедельный отчёт</b>\nДо %s · последние %d дней", captionDate, lookback)); err != nil {
		return err
	}
	return a.Messenger.SendPhoto(ctx, input.ChatID, weightPNG, fmt.Sprintf("<b>Вес · еженедельный отчёт</b>\nДо %s · последние %d дней", captionDate, lookback))
}

func (a *Activities) RunAgentTurn(ctx context.Context, input AgentTurnInput) error {
	if len(a.Allowed) > 0 && !a.Allowed[input.UserID] {
		if a.Metrics != nil {
			a.Metrics.RecordAgentRequest("none", "rejected")
		}
		if input.DeliverToTelegram {
			return a.SendTelegramMessage(ctx, input.ChatID, "У вас нет доступа к этому боту.")
		}
		return nil
	}
	if input.Text == "/start" {
		if a.Metrics != nil {
			a.Metrics.RecordAgentRequest("temporal", "received")
			a.Metrics.RecordAgentTurn("temporal", "start_command", 0)
		}
		if input.DeliverToTelegram {
			return a.SendTelegramMessage(ctx, input.ChatID, "Тренер по дневнику. Напиши «что на сегодня» — скажу, что уже было, и предложу план. Или сразу запиши подход: «жим 70 3*10/12».")
		}
		return nil
	}
	if a.Agent == nil {
		return errors.New("agent is not configured")
	}
	turnContext := agent.WithMetricsPath(ctx, "temporal")
	if input.DeliverToTelegram && a.Status != nil {
		turnContext = agent.WithTurnStatus(turnContext, a.Status)
	}
	result, err := a.Agent.Turn(turnContext, input.ChatID, input.Text, input.Images, false)
	if err != nil {
		if input.DeliverToTelegram {
			_ = a.SendTelegramMessage(ctx, input.ChatID, "❌ Не удалось обработать запрос. Попробуй ещё раз или переформулируй.")
		}
		return err
	}
	if input.DeliverToTelegram {
		return a.SendTelegramMessage(ctx, input.ChatID, agent.NormalizeReply(result.Reply))
	}
	return nil
}
