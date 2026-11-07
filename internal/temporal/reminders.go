package temporal

import (
	"context"
	"fmt"
	"html"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
)

const (
	reminderCooldownDays = 5
	habitLookbackDays    = 90
	habitGapDays         = 7
	exerciseGapDays      = 21
)

// ReminderState is carried across daily runs of the per-chat workflow.
// The dates identify the gaps already mentioned in Telegram.
type ReminderState struct {
	LastAttempt   string            `json:"lastAttempt,omitempty"`
	WorkoutDate   string            `json:"workoutDate,omitempty"`
	WeightDate    string            `json:"weightDate,omitempty"`
	ExerciseDates map[string]string `json:"exerciseDates,omitempty"`
}

type reminderDecision struct {
	State   ReminderState
	Message string
}

func (a *Activities) ChooseEveningReminder(ctx context.Context, input ReminderInput, scheduledAt time.Time) (reminderDecision, error) {
	today, due, err := a.eveningReminderTime(input, scheduledAt, time.Now())
	if err != nil || !due {
		return reminderDecision{}, err
	}
	grid, err := a.Workout.Grid(ctx)
	if err != nil {
		return reminderDecision{}, err
	}
	weights, err := a.Health.WeightHistory(ctx, habitLookbackDays, today)
	if err != nil {
		return reminderDecision{}, err
	}
	return chooseEveningReminder(today, input.State, grid, weights.Days), nil
}

func (a *Activities) DeliverEveningReminder(ctx context.Context, input ReminderInput, scheduledAt time.Time, message string) (bool, error) {
	_, due, err := a.eveningReminderTime(input, scheduledAt, time.Now())
	if err != nil || !due {
		return false, err
	}
	return true, a.SendTelegramMessage(ctx, input.ChatID, message)
}

func (a *Activities) eveningReminderEnabled(chatID int64) bool {
	return a.Allowed[chatID] && a.ReminderOn != nil && a.ReminderOn()
}

func (a *Activities) eveningReminderTime(input ReminderInput, scheduledAt, now time.Time) (time.Time, bool, error) {
	if !a.eveningReminderEnabled(input.ChatID) {
		return time.Time{}, false, nil
	}
	zone, err := time.LoadLocation(input.ZoneID)
	if err != nil {
		return time.Time{}, false, err
	}
	today := now.In(zone)
	due := !now.Before(scheduledAt) && now.Sub(scheduledAt) < time.Hour &&
		today.Format(time.DateOnly) == scheduledAt.In(zone).Format(time.DateOnly) &&
		reminderDue(today, input.State)
	return today, due, nil
}

func reminderDue(today time.Time, state ReminderState) bool {
	// Saturday already has a scheduled health report.
	return today.Weekday() != time.Saturday &&
		state.LastAttempt <= today.AddDate(0, 0, -reminderCooldownDays).Format(time.DateOnly)
}

func chooseEveningReminder(today time.Time, state ReminderState, grid workout.Grid, weights []health.WeightDay) reminderDecision {
	day := today.Format(time.DateOnly)
	recent := today.AddDate(0, 0, 1-habitLookbackDays).Format(time.DateOnly)
	habitCutoff := today.AddDate(0, 0, -habitGapDays).Format(time.DateOnly)
	exerciseCutoff := today.AddDate(0, 0, -exerciseGapDays).Format(time.DateOnly)

	latestWorkout, workoutDays := "", 0
	for _, date := range grid.Dates {
		if date >= recent && date <= day {
			latestWorkout = max(latestWorkout, date)
			workoutDays++
		}
	}
	latestWeight, weightDays := "", 0
	for _, measurement := range weights {
		if measurement.Date >= recent && measurement.Date <= day {
			latestWeight = max(latestWeight, measurement.Date)
			weightDays++
		}
	}

	var message string
	switch {
	case workoutDays >= 2 && latestWorkout <= habitCutoff && latestWorkout > state.WorkoutDate:
		state.WorkoutDate = latestWorkout
		message = "В дневнике уже неделю тихо. Если захочешь вернуться к тренировкам, могу помочь выбрать, с чего начать."
	case weightDays >= 2 && latestWeight <= habitCutoff && latestWeight > state.WeightDate:
		state.WeightDate = latestWeight
		message = "Давно не записывали вес. Если тебе сейчас удобно взвеситься, пришли цифру — я добавлю её в дневник."
	case latestWorkout > habitCutoff && latestWorkout < day:
		var exercise workout.GridRow
		lastDone := ""
		for _, row := range grid.Rows {
			latest, sessions := "", 0
			for date := range row.Cells {
				if date >= recent && date <= day {
					latest = max(latest, date)
					sessions++
				}
			}
			if sessions >= 3 && latest <= exerciseCutoff && latest > state.ExerciseDates[row.ExerciseID] &&
				(latest > lastDone || latest == lastDone && row.ExerciseID < exercise.ExerciseID) {
				exercise, lastDone = row, latest
			}
		}
		if lastDone != "" {
			if state.ExerciseDates == nil {
				state.ExerciseDates = make(map[string]string)
			}
			state.ExerciseDates[exercise.ExerciseID] = lastDone
			message = fmt.Sprintf("Давно не видел в дневнике «%s». Если это упражнение ещё в твоём плане, можно сделать его на следующей тренировке.", html.EscapeString(exercise.ExerciseName))
		}
	}
	if message != "" {
		state.LastAttempt = day
	}
	return reminderDecision{State: state, Message: message}
}
