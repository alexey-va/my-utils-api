package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5"
)

// ToolAttempt separates a tool's own outcome from persistence of its receipt.
// A non-nil outer error means the receipt transaction could not be completed.
type ToolAttempt struct {
	Result  string
	Err     error
	Message Message
}

type RecordedToolExecutor interface {
	ExecuteRecorded(context.Context, int64, string, map[string]any, string, bool, string) (ToolAttempt, error)
}

type RecordedToolCall struct {
	Name       string
	Arguments  map[string]any
	Content    string
	ToolCallID string
}

type RecordedToolPlanExecutor interface {
	ExecutePlanRecorded(context.Context, int64, []RecordedToolCall, bool) ([]ToolAttempt, error)
}

func (s *ToolService) ExecuteRecorded(ctx context.Context, chatID int64, name string, args map[string]any, content string, sandbox bool, toolCallID string) (ToolAttempt, error) {
	attempts, err := s.ExecutePlanRecorded(ctx, chatID, []RecordedToolCall{{Name: name, Arguments: args, Content: content, ToolCallID: toolCallID}}, sandbox)
	if err != nil {
		if len(attempts) == 1 {
			return attempts[0], err
		}
		return ToolAttempt{}, err
	}
	if len(attempts) != 1 {
		return ToolAttempt{}, errors.New("recorded tool returned an invalid attempt count")
	}
	return attempts[0], nil
}

func (s *ToolService) ExecutePlanRecorded(ctx context.Context, chatID int64, calls []RecordedToolCall, sandbox bool) ([]ToolAttempt, error) {
	if isSandboxID(chatID) != sandbox {
		if sandbox {
			return nil, errors.New("sandbox execution requires a reserved test chat ID")
		}
		return nil, errors.New("reserved sandbox chat cannot execute real tools")
	}
	if len(calls) == 0 {
		return []ToolAttempt{}, nil
	}
	normalized := make([]RecordedToolCall, len(calls))
	hasExternal, hasDatabase := false, false
	for index, call := range calls {
		normalized[index] = call
		normalized[index].Name = NormalizeToolName(call.Name)
		if sandbox || !isExternalTool(normalized[index].Name) {
			hasDatabase = true
		} else {
			hasExternal = true
		}
	}
	if !sandbox && hasExternal && hasDatabase {
		return nil, errors.New("план нельзя выполнить: Telegram/Temporal-действия нельзя смешивать с изменением дневника")
	}
	if hasDatabase {
		return s.executeDatabasePlan(ctx, chatID, normalized, sandbox)
	}
	attempts := make([]ToolAttempt, len(normalized))
	for index, call := range normalized {
		attempt, err := s.executeRecordedOne(ctx, chatID, call.Name, call.Arguments, call.Content, sandbox, call.ToolCallID)
		attempts[index] = attempt
		if err != nil {
			return attempts[:index+1], err
		}
	}
	return attempts, nil
}

func isExternalTool(name string) bool {
	switch NormalizeToolName(name) {
	case "send_notification", "schedule_notification", "cancel_notification", "send_rich_message", "send_progress_chart", "estimate_1rm":
		return true
	default:
		return false
	}
}

func (s *ToolService) executeRecordedOne(ctx context.Context, chatID int64, name string, args map[string]any, content string, sandbox bool, toolCallID string) (ToolAttempt, error) {
	name = NormalizeToolName(name)
	if (sandbox && name != "get_conversation_history") || isDatabaseMutation(name) {
		attempts, err := s.executeDatabasePlan(ctx, chatID, []RecordedToolCall{{Name: name, Arguments: args, Content: content, ToolCallID: toolCallID}}, sandbox)
		if len(attempts) == 1 {
			return attempts[0], err
		}
		return ToolAttempt{}, err
	}
	result, callErr := s.Execute(ctx, chatID, name, args, content, sandbox)
	message, err := s.persistToolReceipt(ctx, chatID, name, args, toolCallID, result, callErr)
	return ToolAttempt{Result: result, Err: callErr, Message: message}, err
}

func isDatabaseMutation(name string) bool {
	switch NormalizeToolName(name) {
	case "create_exercise", "rename_exercise", "log_workout", "copy_workout", "delete_workout",
		"log_body_weight", "remember_fact", "forget_fact", "manage_user_fact":
		return true
	default:
		return false
	}
}

func (s *ToolService) executeDatabasePlan(ctx context.Context, chatID int64, calls []RecordedToolCall, sandbox bool) ([]ToolAttempt, error) {
	if s.pool == nil {
		return nil, errors.New("database is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	attempts := make([]ToolAttempt, len(calls))
	transactional := *s
	if !sandbox {
		if s.workout != nil {
			transactional.workout = s.workout.WithTx(tx)
		}
		if s.health != nil {
			transactional.health = s.health.WithTx(tx)
		}
		if s.memory != nil {
			transactional.memory = s.memory.WithTx(tx)
		}
	}
	for index, call := range calls {
		started := time.Now()
		var result string
		var callErr error
		result, callErr = runRecordedAction(func() (string, error) {
			if sandbox {
				if call.Name == "get_conversation_history" {
					if s.memory == nil {
						return "", errors.New("agent memory is not configured")
					}
					var exists bool
					if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_test_sandbox_states WHERE memory_chat_id=$1)`, chatID).Scan(&exists); err != nil {
						return "", err
					}
					if !exists {
						return "", errors.New("sandbox state not found; refusing real-data fallback")
					}
					return s.memory.WithTx(tx).ConversationHistory(ctx, chatID, int64(optionalInt(call.Arguments, "before_id", 0, 0, 2_147_483_647)), optionalInt(call.Arguments, "limit", 100, 1, 100))
				}
				return s.runSandboxRecordedTx(ctx, tx, chatID, call.Name, call.Arguments)
			}
			return transactional.executeReal(ctx, chatID, call.Name, call.Arguments)
		})
		if s.metrics != nil {
			status := "success"
			if callErr != nil {
				status = "error"
			}
			s.metrics.RecordTool(metricsPath(ctx, sandbox), call.Name, status, time.Since(started))
		}
		attempts[index] = ToolAttempt{Result: result, Err: callErr}
		if callErr != nil {
			// Roll back before persisting failed attempts. PostgreSQL transactions that
			// failed a statement cannot reliably accept receipt inserts; no DB write
			// from this plan is allowed to survive.
			rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
			if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return attempts, rollbackErr
			}
			for previous := range attempts {
				if previous == index {
					continue
				}
				if previous < index {
					attempts[previous].Err = fmt.Errorf("план отменён после ошибки действия %q", call.Name)
				} else {
					attempts[previous].Err = fmt.Errorf("действие не выполнялось: предыдущее действие %q завершилось с ошибкой", call.Name)
				}
			}
			return s.persistPlanReceipts(ctx, chatID, calls, attempts)
		}
	}
	for index, call := range calls {
		message, err := s.appendToolReceiptTx(ctx, tx, chatID, call.Name, call.Arguments, call.ToolCallID, attempts[index].Result, nil)
		if err != nil {
			for index := range attempts {
				attempts[index].Message = Message{}
			}
			return attempts, err
		}
		attempts[index].Message = message
	}
	if err := ctx.Err(); err != nil {
		return attempts, err
	}
	// Once finalization starts, an HTTP disconnect must not interrupt COMMIT
	// halfway through. Bound it independently and recover durable receipts on
	// an ambiguous transport error; never replay a mutation to discover its state.
	commitCtx, cancelCommit := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelCommit()
	if err := tx.Commit(commitCtx); err != nil {
		if recovered, ok := s.readCommittedReceipts(ctx, chatID, attempts); ok {
			return recovered, nil
		}
		for index := range attempts {
			attempts[index].Message = Message{}
		}
		return attempts, err
	}
	return attempts, nil
}

func (s *ToolService) readCommittedReceipts(ctx context.Context, chatID int64, attempts []ToolAttempt) ([]ToolAttempt, bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	ids := make([]int64, len(attempts))
	for i := range attempts {
		ids[i] = attempts[i].Message.ID
	}
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM agent_conversation_messages WHERE chat_id=$1 AND id=ANY($2::bigint[])`, chatID, ids).Scan(&count)
	return attempts, err == nil && count == len(attempts)
}

func runRecordedAction(action func() (string, error)) (result string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if argument, ok := recovered.(argumentPanic); ok {
				result, err = "", argument
				return
			}
			panic(recovered)
		}
	}()
	return action()
}

func (s *ToolService) persistPlanReceipts(ctx context.Context, chatID int64, calls []RecordedToolCall, attempts []ToolAttempt) ([]ToolAttempt, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return attempts, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for index, call := range calls {
		message, err := s.appendToolReceiptTx(ctx, tx, chatID, call.Name, call.Arguments, call.ToolCallID, attempts[index].Result, attempts[index].Err)
		if err != nil {
			return attempts, err
		}
		attempts[index].Message = message
	}
	return attempts, tx.Commit(ctx)
}

func (s *ToolService) runSandboxRecordedTx(ctx context.Context, tx pgx.Tx, chatID int64, name string, args map[string]any) (string, error) {
	var raw string
	if err := tx.QueryRow(ctx, `SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1 FOR UPDATE`, chatID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("sandbox state not found; refusing real-data fallback")
		}
		return "", err
	}
	state := sandboxState{}
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return "", fmt.Errorf("decode sandbox state: %w", err)
		}
	}
	result, err := s.runSandboxTool(&state, name, args)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_test_sandbox_states SET state_json=$2,version=version+1,updated_at=now() WHERE memory_chat_id=$1`, chatID, string(encoded)); err != nil {
		return "", err
	}
	return result, nil
}

func (s *ToolService) persistToolReceipt(ctx context.Context, chatID int64, name string, args map[string]any, toolCallID, result string, callErr error) (Message, error) {
	if s.memory == nil || s.memory.db == nil {
		return Message{}, errors.New("agent memory is not configured")
	}
	return appendToolReceipt(ctx, s.memory.db, chatID, name, args, toolCallID, result, callErr)
}

func (s *ToolService) appendToolReceiptTx(ctx context.Context, tx pgx.Tx, chatID int64, name string, args map[string]any, toolCallID, result string, callErr error) (Message, error) {
	return appendToolReceipt(ctx, tx, chatID, name, args, toolCallID, result, callErr)
}

func appendToolReceipt(ctx context.Context, db queryer, chatID int64, name string, args map[string]any, toolCallID, result string, callErr error) (Message, error) {
	receipt := map[string]any{"ok": callErr == nil, "tool": name, "arguments": args}
	if callErr != nil {
		receipt["error"] = callErr.Error()
	} else {
		receipt["result"] = result
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return Message{}, err
	}
	message := openrouter.Message{Role: "tool", Content: string(raw), ToolCallID: toolCallID, Name: name}
	if name == "get_conversation_history" && callErr == nil {
		message.Content = `{"ok":true,"result":"Архив прочитан. Для точных прошлых сообщений повторно вызови get_conversation_history."}`
	}
	return appendMessageTo(ctx, db, chatID, message)
}
