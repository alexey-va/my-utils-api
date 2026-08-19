package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Compactor struct {
	pool   *pgxpool.Pool
	client Completer
	model  func() string
}

func NewCompactor(pool *pgxpool.Pool, client Completer, model func() string) *Compactor {
	return &Compactor{pool: pool, client: client, model: model}
}

type compactMessage struct {
	id   int64
	raw  string
	role string
}

func (c *Compactor) Compact(ctx context.Context, chatID int64, keepRecent int) (CompactResult, error) {
	if keepRecent < 0 {
		keepRecent = 0
	}
	return c.compact(ctx, chatID, func(count int) int { return max(0, count-keepRecent) })
}

func (c *Compactor) CompactAuto(ctx context.Context, chatID int64, keepRecent, threshold int) (CompactResult, error) {
	keepRecent = max(0, keepRecent)
	threshold = max(0, threshold)
	return c.compact(ctx, chatID, func(count int) int {
		selected := count - keepRecent
		if selected <= threshold {
			return 0
		}
		return selected
	})
}

func (c *Compactor) compact(ctx context.Context, chatID int64, selectedCount func(int) int) (CompactResult, error) {
	connection, err := c.pool.Acquire(ctx)
	if err != nil {
		return CompactResult{}, err
	}
	defer connection.Release()
	var locked bool
	if err := connection.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, chatID).Scan(&locked); err != nil {
		return CompactResult{}, err
	}
	if !locked {
		reason := "already_running"
		return CompactResult{Reason: &reason}, nil
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.Exec(unlockContext, `SELECT pg_advisory_unlock($1)`, chatID)
	}()

	rows, err := connection.Query(ctx, `SELECT id,message_json FROM agent_conversation_messages WHERE chat_id=$1 AND NOT excluded_from_context AND NOT is_compacted AND COALESCE(message_json::jsonb->>'role','') <> 'system' ORDER BY created_at,id`, chatID)
	if err != nil {
		return CompactResult{}, err
	}
	messages := []compactMessage{}
	for rows.Next() {
		var row compactMessage
		if err := rows.Scan(&row.id, &row.raw); err != nil {
			rows.Close()
			return CompactResult{}, err
		}
		row.role = roleFromJSON(row.raw)
		messages = append(messages, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return CompactResult{}, err
	}
	selected := selectedCount(len(messages))
	selected = rewindSplitToolTurn(messages, selected)
	if selected <= 0 {
		return CompactResult{Compacted: false}, nil
	}
	toCompact := messages[:selected]
	var previous string
	if err := connection.QueryRow(ctx, `SELECT summary_text FROM agent_context_summaries WHERE chat_id=$1`, chatID).Scan(&previous); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CompactResult{}, err
	}
	var prompt strings.Builder
	if strings.TrimSpace(previous) != "" {
		prompt.WriteString("Текущий накопленный summary:\n")
		prompt.WriteString(previous)
		prompt.WriteString("\n\n")
	}
	prompt.WriteString("Новые сообщения для добавления в summary:\n")
	for _, message := range toCompact {
		fmt.Fprintf(&prompt, "--- id=%d\n%s\n", message.id, message.raw)
	}
	model := c.model()
	response, err := c.client.Complete(ctx, openrouter.Request{Model: model, Messages: []openrouter.Message{
		{Role: "system", Content: compactionPrompt},
		{Role: "user", Content: prompt.String()},
	}})
	if err != nil {
		return CompactResult{}, fmt.Errorf("compact with OpenRouter: %w", err)
	}
	summary := strings.TrimSpace(contentString(response.Message.Content))
	if summary == "" {
		return CompactResult{}, fmt.Errorf("модель вернула пустой summary")
	}

	tx, err := connection.Begin(ctx)
	if err != nil {
		return CompactResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	ids := make([]int64, len(toCompact))
	for index, message := range toCompact {
		ids[index] = message.id
	}
	var stillEligible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_conversation_messages WHERE id=ANY($1::bigint[]) AND chat_id=$2 AND NOT excluded_from_context AND NOT is_compacted`, ids, chatID).Scan(&stillEligible); err != nil {
		return CompactResult{}, err
	}
	if stillEligible != len(ids) {
		reason := "selection_changed"
		return CompactResult{Reason: &reason}, nil
	}
	var summaryID string
	err = tx.QueryRow(ctx, `
		INSERT INTO agent_context_summaries(id,chat_id,sequence,summary_text,covers_message_id_from,covers_message_id_to,source_message_count,model,tokens_before,tokens_after)
		VALUES(gen_random_uuid(),$1,1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT(chat_id) DO UPDATE SET
			summary_text=excluded.summary_text,
			covers_message_id_from=LEAST(agent_context_summaries.covers_message_id_from,excluded.covers_message_id_from),
			covers_message_id_to=GREATEST(agent_context_summaries.covers_message_id_to,excluded.covers_message_id_to),
			source_message_count=agent_context_summaries.source_message_count+excluded.source_message_count,
			model=excluded.model,tokens_before=excluded.tokens_before,tokens_after=excluded.tokens_after,created_at=now()
		RETURNING id::text
	`, chatID, summary, toCompact[0].id, toCompact[len(toCompact)-1].id, len(toCompact), model, prompt.Len(), len(summary)).Scan(&summaryID)
	if err != nil {
		return CompactResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_conversation_messages SET compacted_into_summary_id=$1::uuid,is_compacted=true WHERE id=ANY($2::bigint[])`, summaryID, ids); err != nil {
		return CompactResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CompactResult{}, err
	}
	return CompactResult{Compacted: true, MessageCount: len(toCompact), SummaryID: &summaryID}, nil
}

func roleFromJSON(raw string) string {
	var value struct {
		Role string `json:"role"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(value.Role))
}

func rewindSplitToolTurn(messages []compactMessage, selected int) int {
	selected = min(max(selected, 0), len(messages))
	if selected == 0 || selected == len(messages) || messages[selected].role != "tool" {
		return selected
	}
	boundary := selected
	for boundary > 0 && messages[boundary-1].role == "tool" {
		boundary--
	}
	if boundary > 0 && messages[boundary-1].role == "assistant" {
		boundary--
	}
	return boundary
}

const compactionPrompt = `Обнови единый накопительный summary диалога агента с пользователем на русском.
Верни один цельный summary: объедини прежний summary с новыми сообщениями, убери повторы и замени устаревшие договорённости более свежими.
Сохрани темы, решения, числа, абсолютные даты и просьбы пользователя.
Не используй без абсолютной даты относительные формулировки «сегодня», «вчера», «эта неделя».
Не сохраняй как актуальный факт переходящие выводы о текущей неделе: её состояние приходит агенту отдельно.
Не дублируй долгосрочные факты о пользователе — они хранятся отдельно.
Формат: короткие буллеты, без воды.`
