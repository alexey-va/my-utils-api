package report

import (
	"context"
	"fmt"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
)

type HealthHistory interface {
	StepsHistory(context.Context, int, time.Time) (health.StepsHistory, error)
	WeightHistory(context.Context, int, time.Time) (health.WeightHistory, error)
}

type WeeklyHealthRenderer interface {
	RenderSteps([]health.StepDay, time.Time, time.Time) ([]byte, error)
	RenderWeight([]health.WeightDay, time.Time, time.Time) ([]byte, error)
}

func SendWeeklyHealthReport(ctx context.Context, history HealthHistory, renderer WeeklyHealthRenderer, messenger Messenger, chatID int64, reportDate time.Time, lookback int) error {
	lookback = min(max(lookback, 7), 366)
	from := reportDate.AddDate(0, 0, -(lookback - 1))
	steps, err := history.StepsHistory(ctx, lookback, reportDate)
	if err != nil {
		return err
	}
	weights, err := history.WeightHistory(ctx, lookback, reportDate)
	if err != nil {
		return err
	}
	stepsPNG, err := renderer.RenderSteps(steps.Days, from, reportDate)
	if err != nil {
		return err
	}
	weightPNG, err := renderer.RenderWeight(weights.Days, from, reportDate)
	if err != nil {
		return err
	}
	captionDate := reportDate.Format("02.01.2006")
	if err := messenger.SendPhoto(ctx, chatID, stepsPNG, fmt.Sprintf("<b>Шаги · еженедельный отчёт</b>\nДо %s · последние %d дней", captionDate, lookback)); err != nil {
		return err
	}
	return messenger.SendPhoto(ctx, chatID, weightPNG, fmt.Sprintf("<b>Вес · еженедельный отчёт</b>\nДо %s · последние %d дней", captionDate, lookback))
}
