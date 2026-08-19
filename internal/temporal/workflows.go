package temporal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
	_ "time/tzdata"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	SendTelegramMessageActivity = "SendTelegramMessage"
	HasWorkoutTodayActivity     = "HasWorkoutToday"
	SendEveningReminderActivity = "SendEveningReminder"
	SendWeeklyReportActivity    = "SendWeeklyReport"
	RunAgentTurnActivity        = "RunAgentTurn"

	// Unlike the Kotlin workflow, the Go workflow executes the full multi-step
	// turn in one activity so a retry cannot replay already-persisted mutations.
	// Keep enough room for the configured 30 LLM/tool iterations and provider
	// retries instead of truncating an otherwise valid turn after five minutes.
	agentTurnActivityTimeout = 6 * time.Hour
)

type NotificationInput struct {
	ChatID               int64  `json:"chatId"`
	Message              string `json:"message"`
	DeliverAtEpochMillis int64  `json:"deliverAtEpochMillis"`
}

type ReminderInput struct {
	ChatID int64  `json:"chatId"`
	ZoneID string `json:"zoneId"`
	Hour   int    `json:"hour"`
	Minute int    `json:"minute"`
}

type WeeklyReportInput struct {
	ChatID       int64  `json:"chatId"`
	ZoneID       string `json:"zoneId"`
	LookbackDays int    `json:"lookbackDays"`
}

type WeeklyReportActivityInput struct {
	ChatID       int64  `json:"chatId"`
	ReportDate   string `json:"reportDate"`
	LookbackDays int    `json:"lookbackDays"`
}

type AgentTurnInput struct {
	ChatID            int64    `json:"chatId"`
	UserID            int64    `json:"userId"`
	Text              string   `json:"text"`
	Images            []string `json:"images,omitempty"`
	DeliverToTelegram bool     `json:"deliverToTelegram"`
}

func NotificationWorkflow(ctx workflow.Context, input NotificationInput) error {
	delay := time.UnixMilli(input.DeliverAtEpochMillis).Sub(workflow.Now(ctx))
	if delay > 0 {
		if err := workflow.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	return workflow.ExecuteActivity(withActivityOptions(ctx, 2*time.Minute, 3), SendTelegramMessageActivity, input.ChatID, input.Message).Get(ctx, nil)
}

func EveningReminderWorkflow(ctx workflow.Context, input ReminderInput) error {
	location, err := time.LoadLocation(input.ZoneID)
	if err != nil {
		return fmt.Errorf("load reminder zone: %w", err)
	}
	now := workflow.Now(ctx).In(location)
	target := time.Date(now.Year(), now.Month(), now.Day(), input.Hour, input.Minute, 0, 0, location)
	if !target.After(now) {
		target = target.AddDate(0, 0, 1)
	}
	if err := workflow.Sleep(ctx, target.Sub(now)); err != nil {
		return err
	}
	activityCtx := withActivityOptions(ctx, 2*time.Minute, 3)
	var logged bool
	if err := workflow.ExecuteActivity(activityCtx, HasWorkoutTodayActivity, input.ZoneID).Get(ctx, &logged); err != nil {
		return err
	}
	if !logged {
		if err := workflow.ExecuteActivity(activityCtx, SendEveningReminderActivity, input.ChatID).Get(ctx, nil); err != nil {
			return err
		}
	}
	return workflow.NewContinueAsNewError(ctx, EveningReminderWorkflow, input)
}

func WeeklyReportWorkflow(ctx workflow.Context, input WeeklyReportInput) error {
	location, err := time.LoadLocation(input.ZoneID)
	if err != nil {
		return fmt.Errorf("load report zone: %w", err)
	}
	now := workflow.Now(ctx).In(location)
	target := NextSaturdayNoon(now)
	if err := workflow.Sleep(ctx, target.Sub(now)); err != nil {
		return err
	}
	lookback := min(max(input.LookbackDays, 7), 366)
	activityInput := WeeklyReportActivityInput{ChatID: input.ChatID, ReportDate: target.Format(time.DateOnly), LookbackDays: lookback}
	if err := workflow.ExecuteActivity(withActivityOptions(ctx, 5*time.Minute, 3), SendWeeklyReportActivity, activityInput).Get(ctx, nil); err != nil {
		return err
	}
	return workflow.NewContinueAsNewError(ctx, WeeklyReportWorkflow, input)
}

func AgentTurnWorkflow(ctx workflow.Context, input AgentTurnInput) error {
	// A whole turn may already have persisted tool mutations before delivery
	// fails. Do not replay it automatically and duplicate user data.
	return workflow.ExecuteActivity(withActivityOptions(ctx, agentTurnActivityTimeout, 1), RunAgentTurnActivity, input).Get(ctx, nil)
}

func withActivityOptions(ctx workflow.Context, timeout time.Duration, attempts int32) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: timeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: attempts},
	})
}

func NextSaturdayNoon(now time.Time) time.Time {
	days := (int(time.Saturday) - int(now.Weekday()) + 7) % 7
	target := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location()).AddDate(0, 0, days)
	if !target.After(now) {
		target = target.AddDate(0, 0, 7)
	}
	return target
}

func EveningReminderWorkflowID(chatID int64) string {
	return fmt.Sprintf("go-v1-evening-reminder-%d", chatID)
}
func WeeklyReportWorkflowID(chatID int64) string {
	return fmt.Sprintf("go-v1-weekly-health-report-%d", chatID)
}
func NotificationWorkflowID(chatID int64) string {
	return fmt.Sprintf("go-v1-tg-notify-%d-%s", chatID, randomSuffix())
}
func AgentTurnWorkflowID(chatID int64) string {
	return fmt.Sprintf("go-v1-agent-turn-%d-%s", chatID, randomSuffix())
}

func randomSuffix() string {
	buffer := make([]byte, 12)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
