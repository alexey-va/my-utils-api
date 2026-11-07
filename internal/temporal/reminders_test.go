package temporal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestEveningReminderUsesDiaryGaps(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, time.November, 9, 20, 0, 0, 0, time.UTC)
	grid := workout.Grid{
		Dates: []string{"2026-11-20", "2026-10-19", "2026-10-15", "2026-10-08"},
		Rows: []workout.GridRow{{
			ExerciseID: "press", ExerciseName: "Жим <гантелей>",
			Cells: map[string]workout.Cell{"2026-10-19": {}, "2026-10-15": {}, "2026-10-08": {}},
		}},
	}
	weights := []health.WeightDay{{Date: "2026-10-27"}, {Date: "2026-11-02"}, {Date: "2026-11-11"}}

	first := chooseEveningReminder(today, ReminderState{}, grid, weights)
	if first.State.WorkoutDate != "2026-10-19" || first.State.LastAttempt != "2026-11-09" {
		t.Fatalf("expected workout nudge first, got %#v", first)
	}
	if reminderDue(today.AddDate(0, 0, 4), first.State) {
		t.Fatal("four days must still be in cooldown")
	}
	if reminderDue(today.AddDate(0, 0, 5), first.State) {
		t.Fatal("Saturday is reserved for the health report")
	}
	later := today.AddDate(0, 0, 6)
	if !reminderDue(later, first.State) {
		t.Fatal("cooldown should have elapsed")
	}
	second := chooseEveningReminder(later, first.State, grid, weights)
	if second.Message != "" {
		t.Fatalf("old workout gap already mentioned, weight logged recently: %#v", second)
	}
	weight := chooseEveningReminder(today, first.State, grid, weights)
	if weight.State.WeightDate != "2026-11-02" {
		t.Fatalf("expected a weight nudge, got %#v", weight)
	}
	grid.Dates = append(grid.Dates, "2026-11-06")
	exercise := chooseEveningReminder(today, weight.State, grid, weights)
	if exercise.State.ExerciseDates["press"] != "2026-10-19" || !strings.Contains(exercise.Message, "Жим &lt;гантелей&gt;") {
		t.Fatalf("expected a nudge about the previous exercise, got %#v", exercise)
	}
	if next := chooseEveningReminder(today, exercise.State, grid, weights); next.Message != "" {
		t.Fatalf("the same exercise gap must not repeat: %#v", next)
	}
	grid.Dates = append(grid.Dates, "2026-11-08")
	newGap := chooseEveningReminder(today.AddDate(0, 0, 7), exercise.State, grid, weights)
	if newGap.State.WorkoutDate != "2026-11-08" {
		t.Fatalf("new workouts should allow a later nudge for a different gap: %#v", newGap)
	}
	if empty := chooseEveningReminder(today, ReminderState{}, workout.Grid{Dates: []string{"2026-11-10", "2026-09-01"}}, nil); empty.Message != "" {
		t.Fatalf("a single past session should not establish a habit: %#v", empty)
	}
}

func TestEveningReminderCarriesCooldownEvenOnDeliveryFailure(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, time.November, 9, 19, 0, 0, 0, time.UTC))
	want := ReminderState{LastAttempt: "2026-11-09", WeightDate: "2026-11-02"}
	env.RegisterActivityWithOptions(func(_ context.Context, input ReminderInput, at time.Time) (reminderDecision, error) {
		if input.ChatID != 42 || at.Format(time.DateTime) != "2026-11-09 20:00:00" {
			return reminderDecision{}, errors.New("wrong reminder target")
		}
		return reminderDecision{State: want, Message: "Напоминание"}, nil
	}, activity.RegisterOptions{Name: ChooseEveningReminderActivity})
	sends := 0
	env.RegisterActivityWithOptions(func(_ context.Context, input ReminderInput, _ time.Time, message string) (bool, error) {
		if input.ChatID == 42 && message == "Напоминание" {
			sends++
		}
		return true, errors.New("delivery outcome unknown")
	}, activity.RegisterOptions{Name: DeliverEveningReminderActivity})

	env.ExecuteWorkflow(EveningReminderWorkflow, ReminderInput{ChatID: 42, ZoneID: "UTC", Hour: 20})
	var continued *workflow.ContinueAsNewError
	if err := env.GetWorkflowError(); !errors.As(err, &continued) || sends != 1 {
		t.Fatalf("expected continue-as-new after one send attempt, got error %v and %d sends", err, sends)
	}
	var next ReminderInput
	if err := converter.GetDefaultDataConverter().FromPayloads(continued.Input, &next); err != nil {
		t.Fatal(err)
	}
	if next.State.LastAttempt != want.LastAttempt || next.State.WeightDate != want.WeightDate {
		t.Fatalf("next state = %#v, want %#v", next.State, want)
	}
}

func TestEveningReminderRechecksDeliveryWindowAndSwitch(t *testing.T) {
	t.Parallel()
	enabled := true
	a := &Activities{Allowed: map[int64]bool{42: true}, ReminderOn: func() bool { return enabled }}
	input := ReminderInput{ChatID: 42, ZoneID: "UTC", State: ReminderState{LastAttempt: "2026-11-08"}}
	friday := time.Date(2026, time.November, 13, 23, 50, 0, 0, time.UTC)
	due := func(now time.Time) bool {
		t.Helper()
		_, allowed, err := a.eveningReminderTime(input, friday, now)
		if err != nil {
			t.Fatal(err)
		}
		return allowed
	}
	if !due(friday) || due(friday.Add(15*time.Minute)) {
		t.Fatal("the Friday decision may be delivered on Friday only")
	}
	enabled = false
	if due(friday) {
		t.Fatal("delivery must stop as soon as reminders are disabled")
	}
	enabled = true
	input.State.LastAttempt = "2026-11-12"
	if due(friday) {
		t.Fatal("re-enabling must respect the previously recorded cooldown")
	}
	input.State = ReminderState{}
	delete(a.Allowed, input.ChatID)
	if due(friday) {
		t.Fatal("delivery must stop if the recipient is no longer allowed")
	}
}
