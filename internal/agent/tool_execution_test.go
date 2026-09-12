package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExecuteRecordedSandboxReceiptFailureRollsBackMutation(t *testing.T) {
	pool, ctx := recordedToolTestPool(t)
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "recorded sandbox rollback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	if _, err := tools.Execute(ctx, chat.MemoryChatID, "create_exercise", map[string]any{"name": "Recorded Sandbox Bench", "muscle_group": "chest"}, "fixture", true); err != nil {
		t.Fatal(err)
	}
	cleanupReceiptFailure(t, pool, ctx, chat.MemoryChatID)

	attempt, outerErr := tools.ExecuteRecorded(ctx, chat.MemoryChatID, "log_workout", map[string]any{
		"exercise_name": "Recorded Sandbox Bench", "notation": "50 8/8", "date": "2026-09-12",
	}, "fixture", true, "call-rollback")
	if outerErr == nil || attempt.Message.ID != 0 {
		t.Fatalf("attempt=%+v outerErr=%v; expected receipt persistence failure", attempt, outerErr)
	}
	var raw string
	if err := pool.QueryRow(ctx, `SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1`, chat.MemoryChatID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state sandboxState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Workouts) != 0 {
		t.Fatalf("sandbox mutation survived receipt failure: %+v", state.Workouts)
	}
}

func TestExecutePlanRecordedSandboxReceiptFailureRollsBackAllMutations(t *testing.T) {
	pool, ctx := recordedToolTestPool(t)
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "recorded sandbox plan rollback")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	if _, err := tools.Execute(ctx, chat.MemoryChatID, "create_exercise", map[string]any{"name": "Recorded Plan Bench", "muscle_group": "chest"}, "fixture", true); err != nil {
		t.Fatal(err)
	}
	cleanupReceiptFailure(t, pool, ctx, chat.MemoryChatID)

	attempts, outerErr := tools.ExecutePlanRecorded(ctx, chat.MemoryChatID, []RecordedToolCall{
		{Name: "log_workout", Arguments: map[string]any{"exercise_name": "Recorded Plan Bench", "notation": "50 8/8", "date": "2026-09-12"}, ToolCallID: "plan-workout"},
		{Name: "log_body_weight", Arguments: map[string]any{"weight_kg": 80, "date": "2026-09-12"}, ToolCallID: "plan-weight"},
	}, true)
	if outerErr == nil || len(attempts) != 2 {
		t.Fatalf("attempts=%+v outerErr=%v; expected plan receipt failure", attempts, outerErr)
	}
	var raw string
	if err := pool.QueryRow(ctx, `SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1`, chat.MemoryChatID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state sandboxState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Workouts) != 0 || len(state.BodyWeights) != 0 {
		t.Fatalf("sandbox plan mutation survived receipt failure: %+v", state)
	}
}

func TestExecutePlanRecordedRejectsMixedExternalAndDatabaseActions(t *testing.T) {
	tools := NewToolService(nil, nil, nil, nil, nil, nil)
	_, err := tools.ExecutePlanRecorded(context.Background(), 42, []RecordedToolCall{
		{Name: "log_workout", Arguments: map[string]any{"exercise_name": "Bench", "notation": "50 8/8"}},
		{Name: "send_rich_message", Arguments: map[string]any{"text": "done"}},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "нельзя смешивать") {
		t.Fatalf("mixed plan error = %v", err)
	}
}

func TestExecutePlanRecordedEnforcesSandboxChatBoundary(t *testing.T) {
	tools := NewToolService(nil, nil, nil, nil, nil, nil)
	if _, err := tools.ExecutePlanRecorded(context.Background(), uniqueRecordedTestID(), []RecordedToolCall{{Name: "list_exercises"}}, false); err == nil || !strings.Contains(err.Error(), "reserved sandbox") {
		t.Fatalf("reserved real plan error = %v", err)
	}
	if _, err := tools.ExecutePlanRecorded(context.Background(), 42, []RecordedToolCall{{Name: "list_exercises"}}, true); err == nil || !strings.Contains(err.Error(), "requires a reserved") {
		t.Fatalf("ordinary sandbox plan error = %v", err)
	}
}

func TestExecuteRecordedSandboxHistoryRequiresState(t *testing.T) {
	pool, ctx := recordedToolTestPool(t)
	memory := NewMemory(pool, nil)
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	chatID := uniqueRecordedTestID()
	attempt, outerErr := tools.ExecuteRecorded(ctx, chatID, "get_conversation_history", map[string]any{"limit": 10}, "fixture", true, "missing-sandbox-history")
	if outerErr != nil || attempt.Err == nil || !strings.Contains(attempt.Err.Error(), "sandbox state not found") {
		t.Fatalf("attempt=%+v outerErr=%v", attempt, outerErr)
	}
	if attempt.Message.ID != 0 {
		defer func() { _ = memory.DeleteMessage(ctx, attempt.Message.ID) }()
	}
}

func TestExecuteRecordedStoresLinkedSandboxReceipt(t *testing.T) {
	pool, ctx := recordedToolTestPool(t)
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "recorded sandbox success")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	if _, err := tools.Execute(ctx, chat.MemoryChatID, "create_exercise", map[string]any{"name": "Recorded Sandbox Press", "muscle_group": "chest"}, "fixture", true); err != nil {
		t.Fatal(err)
	}

	attempt, outerErr := tools.ExecuteRecorded(ctx, chat.MemoryChatID, "log_workout", map[string]any{
		"exercise_name": "Recorded Sandbox Press", "notation": "52.5 8/8", "date": "2026-09-12",
	}, "fixture", true, "call-success")
	if outerErr != nil || attempt.Err != nil || attempt.Message.ID == 0 {
		t.Fatalf("attempt=%+v outerErr=%v", attempt, outerErr)
	}
	if attempt.Message.ToolCallID == nil || *attempt.Message.ToolCallID != "call-success" {
		t.Fatalf("receipt linkage = %#v", attempt.Message)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(valueOrEmpty(attempt.Message.Content)), &receipt); err != nil || receipt["ok"] != true {
		t.Fatalf("receipt=%v err=%v", receipt, err)
	}
	var raw string
	if err := pool.QueryRow(ctx, `SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1`, chat.MemoryChatID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state sandboxState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Workouts) != 1 || state.Workouts[0].WeightKg != 52.5 {
		t.Fatalf("sandbox workout = %+v", state.Workouts)
	}
}

func TestExecuteRecordedRealReceiptFailureRollsBackDiary(t *testing.T) {
	pool, ctx := recordedToolTestPool(t)
	memory := NewMemory(pool, nil)
	workouts := workout.NewService(pool)
	name := "Recorded Real Bench " + strconv.FormatInt(uniqueRecordedTestID(), 10)
	exercise, err := workouts.CreateExercise(ctx, workout.CreateExerciseRequest{Name: name, MuscleGroup: stringPointer("chest")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workouts.DeleteExercise(ctx, exercise.ID) }()
	chatID := uniqueRecordedRealChatID()
	tools := NewToolService(pool, workouts, health.NewService(pool), memory, nil, nil)
	cleanupReceiptFailure(t, pool, ctx, chatID)

	attempt, outerErr := tools.ExecuteRecorded(ctx, chatID, "log_workout", map[string]any{
		"exercise_name": name, "notation": "47.5 8/8", "date": "2026-09-12",
	}, "fixture", false, "call-real-rollback")
	if outerErr == nil || attempt.Message.ID != 0 {
		t.Fatalf("attempt=%+v outerErr=%v; expected receipt persistence failure", attempt, outerErr)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workout_entries WHERE exercise_id=$1::uuid AND performed_on=$2`, exercise.ID, "2026-09-12").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("real diary mutation survived receipt failure: %d rows", count)
	}
}

func recordedToolTestPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

func cleanupReceiptFailure(t *testing.T, pool *pgxpool.Pool, ctx context.Context, chatID int64, toolCallIDs ...string) {
	t.Helper()
	suffix := strings.TrimPrefix(strconv.FormatInt(chatID, 10), "-")
	functionName := "agent_test_fail_receipt_" + suffix
	triggerName := "agent_test_fail_receipt_trigger_" + suffix
	predicate := ""
	if len(toolCallIDs) > 0 {
		predicate = " AND NEW.message_json::jsonb->>'tool_call_id' = '" + strings.ReplaceAll(toolCallIDs[0], "'", "''") + "'"
	}
	createFunction := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN
	IF NEW.chat_id = %d AND NEW.message_json::jsonb->>'role' = 'tool'%s THEN
		RAISE EXCEPTION 'forced tool receipt insert failure';
	END IF;
	RETURN NEW;
END;
$body$`, functionName, chatID, predicate)
	if _, err := pool.Exec(ctx, createFunction); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON agent_conversation_messages FOR EACH ROW EXECUTE FUNCTION %s()`, triggerName, functionName)); err != nil {
		_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP FUNCTION %s()`, functionName))
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON agent_conversation_messages`, triggerName))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})
}

func uniqueRecordedTestID() int64 {
	return -9_000_000_000_000_000 + int64(os.Getpid())*1000 + int64(len("recorded"))
}

func uniqueRecordedRealChatID() int64 {
	return 9_000_000_000_000_000 + int64(os.Getpid())*1000 + int64(len("recorded"))
}

func stringPointer(value string) *string { return &value }

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestFailedPlanReceiptsCommitTogether(t *testing.T) {
	pool, ctx := recordedToolTestPool(t)
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "failed-plan receipt atomicity")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memory.DeleteTestChat(ctx, chat.ID) })
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	cleanupReceiptFailure(t, pool, ctx, chat.MemoryChatID, "failure-second")
	_, outerErr := tools.ExecutePlanRecorded(ctx, chat.MemoryChatID, []RecordedToolCall{
		{Name: "create_exercise", Arguments: map[string]any{"name": "Atomic fixture"}, ToolCallID: "failure-first"},
		{Name: "copy_workout", Arguments: map[string]any{"exercise_name": "Atomic fixture", "date": "2026-09-12"}, ToolCallID: "failure-second"},
	}, true)
	if outerErr == nil {
		t.Fatal("expected second failure-receipt insertion to fail")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM agent_conversation_messages WHERE chat_id=$1", chat.MemoryChatID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial failure archive persisted: %d receipts", count)
	}
	var raw string
	if err := pool.QueryRow(ctx, "SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1", chat.MemoryChatID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state sandboxState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Exercises) != 0 {
		t.Fatal("creation survived a failed plan")
	}
}
