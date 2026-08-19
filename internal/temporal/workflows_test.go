package temporal

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestWorkflowIDsUseFreshGoNamespace(t *testing.T) {
	t.Parallel()
	if got := EveningReminderWorkflowID(42); got != "go-v1-evening-reminder-42" {
		t.Fatalf("evening id = %q", got)
	}
	if got := WeeklyReportWorkflowID(42); got != "go-v1-weekly-health-report-42" {
		t.Fatalf("weekly id = %q", got)
	}
	for _, id := range []string{NotificationWorkflowID(42), AgentTurnWorkflowID(42)} {
		if len(id) < len("go-v1-") || id[:len("go-v1-")] != "go-v1-" {
			t.Fatalf("fresh id = %q", id)
		}
	}
}

func TestNextSaturdayNoon(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("Europe/Moscow", 3*60*60)
	now := time.Date(2026, time.August, 20, 14, 0, 0, 0, zone) // Thursday
	want := time.Date(2026, time.August, 22, 12, 0, 0, 0, zone)
	if got := NextSaturdayNoon(now); !got.Equal(want) {
		t.Fatalf("next = %s, want %s", got, want)
	}
}

func TestNotificationWorkflowSleepsAndDelivers(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	deliverAt := time.Now().Add(time.Hour).UnixMilli()
	delivered := false
	environment.RegisterActivityWithOptions(func(_ context.Context, chatID int64, message string) error {
		delivered = chatID == 42 && message == "hello"
		return nil
	}, activity.RegisterOptions{Name: SendTelegramMessageActivity})
	environment.ExecuteWorkflow(NotificationWorkflow, NotificationInput{ChatID: 42, Message: "hello", DeliverAtEpochMillis: deliverAt})
	if !environment.IsWorkflowCompleted() || environment.GetWorkflowError() != nil {
		t.Fatalf("workflow error = %v", environment.GetWorkflowError())
	}
	if !delivered {
		t.Fatal("notification activity was not called")
	}
}

func TestAgentTurnWorkflowDoesNotRetryNonIdempotentActivity(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	attempts := 0
	var activityDeadline time.Duration
	environment.RegisterActivityWithOptions(func(ctx context.Context, _ AgentTurnInput) error {
		attempts++
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("agent activity must have a deadline")
		}
		activityDeadline = time.Until(deadline)
		return errors.New("failed after mutation")
	}, activity.RegisterOptions{Name: RunAgentTurnActivity})
	environment.ExecuteWorkflow(AgentTurnWorkflow, AgentTurnInput{ChatID: 42, Text: "record"})
	if environment.GetWorkflowError() == nil {
		t.Fatal("workflow should fail")
	}
	if attempts != 1 {
		t.Fatalf("activity attempts = %d, want 1", attempts)
	}
	if activityDeadline < 5*time.Hour+59*time.Minute {
		t.Fatalf("activity deadline = %s, want about %s", activityDeadline, agentTurnActivityTimeout)
	}
}
