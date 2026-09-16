package report

import (
	"context"
	"fmt"
	"log/slog"
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
	if len(stepsPNG) == 0 {
		return fmt.Errorf("render weekly steps chart: empty PNG")
	}
	weightPNG, err := renderer.RenderWeight(weights.Days, from, reportDate)
	if err != nil {
		return err
	}
	if len(weightPNG) == 0 {
		return fmt.Errorf("render weekly weight chart: empty PNG")
	}
	captionDate := reportDate.Format("02.01.2006")
	if err := messenger.SendPhoto(ctx, chatID, stepsPNG, fmt.Sprintf("<b>Шаги · еженедельный отчёт</b>\nДо %s · последние %d дней", captionDate, lookback)); err != nil {
		return err
	}
	if err := messenger.SendPhoto(ctx, chatID, weightPNG, fmt.Sprintf("<b>Вес · еженедельный отчёт</b>\nДо %s · последние %d дней", captionDate, lookback)); err != nil {
		return err
	}
	slog.InfoContext(ctx, "weekly health report delivered",
		"event", "weekly_health_report",
		"steps_png_bytes", len(stepsPNG),
		"weight_png_bytes", len(weightPNG),
		"telegram_photos_sent", 2,
		"telegram_failures", 0,
	)
	return nil
}
