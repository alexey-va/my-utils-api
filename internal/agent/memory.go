package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	Images     []string   `json:"images"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	ToolCallID *string    `json:"tool_call_id"`
	Name       *string    `json:"name"`
}
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	ID         int64     `json:"id"`
	ChatID     int64     `json:"chatId"`
	Role       string    `json:"role"`
	Content    *string   `json:"content"`
	Images     []string  `json:"images"`
	ToolCallID *string   `json:"toolCallId"`
	ToolName   *string   `json:"toolName"`
	Excluded   bool      `json:"excludedFromContext"`
	SummaryID  *string   `json:"compactedIntoSummaryId"`
	Compacted  bool      `json:"isCompacted"`
	CreatedAt  time.Time `json:"createdAt"`
	RawJSON    string    `json:"rawJson"`
}
type MessagePage struct {
	Messages     []Message `json:"messages"`
	NextBeforeID *int64    `json:"nextBeforeId"`
}
type Fact struct {
	ID         string    `json:"id"`
	ChatID     int64     `json:"chatId"`
	Content    string    `json:"content"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
type Summary struct {
	ID           string    `json:"id"`
	Sequence     int       `json:"sequence"`
	SummaryText  string    `json:"summaryText"`
	CoversFrom   int64     `json:"coversMessageIdFrom"`
	CoversTo     int64     `json:"coversMessageIdTo"`
	SourceCount  int       `json:"sourceMessageCount"`
	Model        *string   `json:"model"`
	TokensBefore *int      `json:"tokensBefore"`
	TokensAfter  *int      `json:"tokensAfter"`
	CreatedAt    time.Time `json:"createdAt"`
}
type ChatSummary struct {
	ChatID         int64      `json:"chatId"`
	MessageCount   int64      `json:"messageCount"`
	FactCount      int64      `json:"factCount"`
	SummaryCount   int64      `json:"summaryCount"`
	LastActivityAt *time.Time `json:"lastActivityAt"`
}
type CompactionPreview struct {
	Available        bool `json:"compactionAvailable"`
	CompactableCount int  `json:"compactableCount"`
}
type ChatDetail struct {
	ChatID                    int64             `json:"chatId"`
	Stats                     ChatSummary       `json:"stats"`
	Summaries                 []Summary         `json:"summaries"`
	Facts                     []Fact            `json:"facts"`
	RecentContextMessageCount int               `json:"recentContextMessageCount"`
	Compaction                CompactionPreview `json:"compaction"`
}
type CompactResult struct {
	Compacted    bool    `json:"compacted"`
	MessageCount int     `json:"messageCount"`
	SummaryID    *string `json:"summaryId"`
	Reason       *string `json:"reason"`
}
type TurnResult struct {
	Reply    string    `json:"reply"`
	Messages []Message `json:"messages"`
}
type TestChat struct {
	ID           string    `json:"id"`
	MemoryChatID int64     `json:"memoryChatId"`
	Sandboxed    bool      `json:"sandboxed"`
	Title        string    `json:"title"`
	MessageCount int64     `json:"messageCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Turner interface {
	Turn(context.Context, int64, string, []string, bool) (TurnResult, error)
}
type Memory struct {
	pool      *pgxpool.Pool
	turner    Turner
	zoneID    func() string
	compactor interface {
		Compact(context.Context, int64, int) (CompactResult, error)
	}
	autoCompactor interface{ Trigger(int64) }
}

func (m *Memory) SetAutoCompactor(compactor interface{ Trigger(int64) }) { m.autoCompactor = compactor }

func NewMemory(pool *pgxpool.Pool, turner Turner) *Memory { return &Memory{pool: pool, turner: turner} }

func (m *Memory) SetTurner(turner Turner)        { m.turner = turner }
func (m *Memory) SetZoneID(zoneID func() string) { m.zoneID = zoneID }
func (m *Memory) SetCompactor(compactor interface {
	Compact(context.Context, int64, int) (CompactResult, error)
}) {
	m.compactor = compactor
}

func (m *Memory) AppendOpenRouter(ctx context.Context, id int64, message openrouter.Message) (Message, error) {
	return m.AppendMessage(ctx, id, message)
}

func (m *Memory) AppendMessage(ctx context.Context, id int64, message openrouter.Message) (Message, error) {
	stored := ChatMessage{Role: message.Role}
	if text := strings.TrimSpace(contentString(message.Content)); text != "" {
		stored.Content = &text
	}
	if parts, ok := message.Content.([]openrouter.ContentPart); ok {
		stored.Content = nil
		for _, part := range parts {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				text := strings.TrimSpace(part.Text)
				stored.Content = &text
			}
			if part.ImageURL != nil && strings.TrimSpace(part.ImageURL.URL) != "" {
				stored.Images = append(stored.Images, strings.TrimSpace(part.ImageURL.URL))
			}
		}
	}
	for _, call := range message.ToolCalls {
		stored.ToolCalls = append(stored.ToolCalls, ToolCall{ID: call.ID, Type: call.Type, Function: ToolFunction{Name: call.Function.Name, Arguments: call.Function.Arguments}})
	}
	if message.ToolCallID != "" {
		stored.ToolCallID = &message.ToolCallID
	}
	if message.Name != "" {
		stored.Name = &message.Name
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return Message{}, err
	}
	result, err := scanMessage(m.pool.QueryRow(ctx, `INSERT INTO agent_conversation_messages(chat_id,message_json) VALUES($1,$2) RETURNING id,chat_id,message_json,excluded_from_context,compacted_into_summary_id::text,is_compacted,created_at`, id, string(raw)))
	if err == nil && m.autoCompactor != nil {
		m.autoCompactor.Trigger(id)
	}
	return result, err
}

// Append implements the turner's Conversation interface.
func (m *Memory) Append(ctx context.Context, id int64, message openrouter.Message) (Message, error) {
	return m.AppendMessage(ctx, id, message)
}

func (m *Memory) Context(ctx context.Context, id int64, limit int) ([]openrouter.Message, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := m.pool.Query(ctx, `SELECT message_json,created_at FROM (SELECT id,message_json,created_at FROM agent_conversation_messages WHERE chat_id=$1 AND NOT excluded_from_context AND NOT is_compacted ORDER BY created_at DESC,id DESC LIMIT $2) recent ORDER BY created_at,id`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []openrouter.Message{}
	location := m.memoryLocation()
	for rows.Next() {
		var raw string
		var createdAt time.Time
		if err := rows.Scan(&raw, &createdAt); err != nil {
			return nil, err
		}
		var stored ChatMessage
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			continue
		}
		// Manual system messages are visible in the admin history, but never become
		// a second model instruction beside the controlled application prompt.
		if strings.EqualFold(strings.TrimSpace(stored.Role), "system") {
			continue
		}
		message := openrouter.Message{Role: stored.Role}
		content := ""
		if stored.Content != nil {
			content = timestampContent(stored.Role, *stored.Content, createdAt, location)
			if content != "" {
				message.Content = content
			}
		}
		if len(stored.Images) > 0 {
			parts := []openrouter.ContentPart{}
			if stored.Content != nil {
				parts = append(parts, openrouter.ContentPart{Type: "text", Text: content})
			}
			for _, image := range stored.Images {
				parts = append(parts, openrouter.ContentPart{Type: "image_url", ImageURL: &openrouter.ImageURL{URL: image}})
			}
			message.Content = parts
		}
		for _, call := range stored.ToolCalls {
			message.ToolCalls = append(message.ToolCalls, openrouter.ToolCall{ID: call.ID, Type: call.Type, Function: openrouter.ToolFunction{Name: call.Function.Name, Arguments: call.Function.Arguments}})
		}
		if stored.ToolCallID != nil {
			message.ToolCallID = *stored.ToolCallID
		}
		if stored.Name != nil {
			message.Name = *stored.Name
		}
		if strings.EqualFold(strings.TrimSpace(stored.Role), "assistant") && content == "" && len(stored.Images) == 0 && len(stored.ToolCalls) == 0 {
			continue
		}
		result = append(result, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dropIncompleteToolTurns(result), nil
}

func (m *Memory) memoryLocation() *time.Location {
	zone := "Europe/Moscow"
	if m.zoneID != nil && strings.TrimSpace(m.zoneID()) != "" {
		zone = strings.TrimSpace(m.zoneID())
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC
	}
	return location
}

func timestampContent(role, content string, createdAt time.Time, location *time.Location) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "assistant" {
		content = stripInternalHistoryPrefix(content)
		if LooksInvalidForRussianUser(content) {
			return ""
		}
		return content
	}
	if role != "user" || strings.TrimSpace(content) == "" {
		return content
	}
	return fmt.Sprintf("[Отправлено %s %s] %s", createdAt.In(location).Format("02.01.2006 15:04"), location.String(), content)
}

func dropIncompleteToolTurns(messages []openrouter.Message) []openrouter.Message {
	result := make([]openrouter.Message, 0, len(messages))
	for index := 0; index < len(messages); {
		message := messages[index]
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			required := make(map[string]bool, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				required[call.ID] = true
			}
			cursor := index + 1
			results := make(map[string]int, len(required))
			unexpected := false
			for cursor < len(messages) && messages[cursor].Role == "tool" {
				id := messages[cursor].ToolCallID
				if !required[id] {
					unexpected = true
				} else {
					results[id]++
				}
				cursor++
			}
			complete := !unexpected
			for id := range required {
				complete = complete && results[id] == 1
			}
			if complete {
				result = append(result, messages[index:cursor]...)
			}
			index = cursor
			continue
		}
		if message.Role != "tool" {
			result = append(result, message)
		}
		index++
	}
	return result
}

func (m *Memory) PromptContext(ctx context.Context, id int64) (string, error) {
	var summary string
	if err := m.pool.QueryRow(ctx, `SELECT summary_text FROM agent_context_summaries WHERE chat_id=$1 ORDER BY sequence DESC LIMIT 1`, id).Scan(&summary); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	facts, err := m.facts(ctx, id)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	if strings.TrimSpace(summary) != "" {
		builder.WriteString("[Исторический контекст диалога (сжато, блок 1)]\n")
		builder.WriteString("Это не источник текущей даты, текущей недели или актуального состояния дневника.\n")
		builder.WriteString("Для «сегодня / вчера / эта неделя / уже сделано / осталось» используй только свежий снимок из основного system-сообщения.\n")
		builder.WriteString(strings.TrimSpace(summary))
	}
	if len(facts) > 0 {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("Известные факты о пользователе:\n")
		for _, fact := range facts {
			builder.WriteString("- [")
			builder.WriteString(fact.ID)
			builder.WriteString("] ")
			builder.WriteString(fact.Content)
			builder.WriteByte('\n')
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func (m *Memory) ListChats(ctx context.Context) ([]ChatSummary, error) {
	rows, err := m.pool.Query(ctx, `SELECT chat_id FROM (SELECT DISTINCT chat_id FROM agent_conversation_messages UNION SELECT DISTINCT chat_id FROM agent_user_facts) c ORDER BY chat_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ChatSummary{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		summary, err := m.chatSummary(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, rows.Err()
}
func (m *Memory) chatSummary(ctx context.Context, id int64) (ChatSummary, error) {
	var result ChatSummary
	result.ChatID = id
	err := m.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_conversation_messages WHERE chat_id=$1),(SELECT count(*) FROM agent_user_facts WHERE chat_id=$1),(SELECT count(*) FROM agent_context_summaries WHERE chat_id=$1),(SELECT max(created_at) FROM agent_conversation_messages WHERE chat_id=$1)`, id).Scan(&result.MessageCount, &result.FactCount, &result.SummaryCount, &result.LastActivityAt)
	return result, err
}
func (m *Memory) Detail(ctx context.Context, id int64) (ChatDetail, error) {
	stats, err := m.chatSummary(ctx, id)
	if err != nil {
		return ChatDetail{}, err
	}
	summaries, err := m.summaries(ctx, id)
	if err != nil {
		return ChatDetail{}, err
	}
	facts, err := m.facts(ctx, id)
	if err != nil {
		return ChatDetail{}, err
	}
	var recent, compactable int
	if err := m.pool.QueryRow(ctx, `SELECT count(*) FROM agent_conversation_messages WHERE chat_id=$1`, id).Scan(&recent); err != nil {
		return ChatDetail{}, err
	}
	if err := m.pool.QueryRow(ctx, `SELECT count(*) FROM agent_conversation_messages WHERE chat_id=$1 AND NOT excluded_from_context AND NOT is_compacted`, id).Scan(&compactable); err != nil {
		return ChatDetail{}, err
	}
	return ChatDetail{ChatID: id, Stats: stats, Summaries: summaries, Facts: facts, RecentContextMessageCount: recent, Compaction: CompactionPreview{Available: m.compactor != nil, CompactableCount: compactable}}, nil
}
func (m *Memory) Messages(ctx context.Context, id int64, before *int64, limit int) (MessagePage, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := m.pool.Query(ctx, `SELECT id,chat_id,message_json,excluded_from_context,compacted_into_summary_id::text,is_compacted,created_at FROM agent_conversation_messages WHERE chat_id=$1 AND ($2::bigint IS NULL OR id<$2) ORDER BY created_at DESC LIMIT $3`, id, before, limit)
	if err != nil {
		return MessagePage{}, err
	}
	defer rows.Close()
	result := MessagePage{Messages: []Message{}}
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return MessagePage{}, err
		}
		result.Messages = append(result.Messages, message)
	}
	if len(result.Messages) == limit {
		value := result.Messages[len(result.Messages)-1].ID
		result.NextBeforeID = &value
	}
	return result, rows.Err()
}
func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var result Message
	err := row.Scan(&result.ID, &result.ChatID, &result.RawJSON, &result.Excluded, &result.SummaryID, &result.Compacted, &result.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	var parsed ChatMessage
	if json.Unmarshal([]byte(result.RawJSON), &parsed) == nil {
		result.Role = parsed.Role
		result.Content = parsed.Content
		result.Images = parsed.Images
		result.ToolCallID = parsed.ToolCallID
		result.ToolName = parsed.Name
	} else {
		result.Role = "unknown"
	}
	return result, nil
}
func (m *Memory) AppendManual(ctx context.Context, id int64, role, content string, images []string) (Message, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "user" && role != "assistant" && role != "system" {
		return Message{}, badRequest("role должен быть user, assistant или system.")
	}
	content = strings.TrimSpace(content)
	images = normalizeImages(images)
	if content == "" && len(images) == 0 {
		return Message{}, badRequest("Нужен текст или хотя бы одно изображение.")
	}
	if role == "system" && len(images) > 0 {
		return Message{}, badRequest("System-сообщения не поддерживают изображения.")
	}
	var contentPtr *string
	if content != "" {
		contentPtr = &content
	}
	raw, _ := json.Marshal(ChatMessage{Role: role, Content: contentPtr, Images: nilIfEmpty(images)})
	result, err := scanMessage(m.pool.QueryRow(ctx, `INSERT INTO agent_conversation_messages(chat_id,message_json) VALUES($1,$2) RETURNING id,chat_id,message_json,excluded_from_context,compacted_into_summary_id::text,is_compacted,created_at`, id, string(raw)))
	if err == nil && m.autoCompactor != nil {
		m.autoCompactor.Trigger(id)
	}
	return result, err
}
func (m *Memory) CreateFact(ctx context.Context, id int64, content string, confidence *float64) (Fact, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Fact{}, badRequest("Факт не может быть пустым.")
	}
	value := 0.95
	if confidence != nil {
		value = *confidence
	}
	if value < 0 || value > 1 {
		return Fact{}, badRequest("confidence must be between 0 and 1")
	}
	return scanFact(m.pool.QueryRow(ctx, `INSERT INTO agent_user_facts(id,chat_id,content,confidence) VALUES(gen_random_uuid(),$1,$2,$3) RETURNING id::text,chat_id,content,confidence,created_at,updated_at`, id, content, value))
}
func (m *Memory) UpdateFact(ctx context.Context, id, content string, confidence *float64) (Fact, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Fact{}, badRequest("Факт не может быть пустым.")
	}
	if confidence != nil && (*confidence < 0 || *confidence > 1) {
		return Fact{}, badRequest("confidence must be between 0 and 1")
	}
	value, err := scanFact(m.pool.QueryRow(ctx, `UPDATE agent_user_facts SET content=$2,confidence=COALESCE($3,confidence),updated_at=now() WHERE id=$1::uuid RETURNING id::text,chat_id,content,confidence,created_at,updated_at`, id, content, confidence))
	if errors.Is(err, pgx.ErrNoRows) {
		return Fact{}, notFound("Факт не найден.")
	}
	return value, err
}
func scanFact(row interface{ Scan(...any) error }) (Fact, error) {
	var value Fact
	err := row.Scan(&value.ID, &value.ChatID, &value.Content, &value.Confidence, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}
func (m *Memory) DeleteFact(ctx context.Context, id string) error {
	_, err := m.pool.Exec(ctx, `DELETE FROM agent_user_facts WHERE id=$1::uuid`, id)
	return err
}
func (m *Memory) DeleteSummary(ctx context.Context, id string) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := tx.Exec(ctx, `UPDATE agent_conversation_messages SET compacted_into_summary_id=NULL,is_compacted=false WHERE compacted_into_summary_id=$1::uuid`, id)
	_ = result
	if err != nil {
		return err
	}
	deleted, err := tx.Exec(ctx, `DELETE FROM agent_context_summaries WHERE id=$1::uuid`, id)
	if err != nil {
		return err
	}
	if deleted.RowsAffected() == 0 {
		return notFound("Summary не найден.")
	}
	return tx.Commit(ctx)
}
func (m *Memory) ExcludeMessage(ctx context.Context, id int64, excluded bool) (Message, error) {
	value, err := scanMessage(m.pool.QueryRow(ctx, `UPDATE agent_conversation_messages SET excluded_from_context=$2 WHERE id=$1 RETURNING id,chat_id,message_json,excluded_from_context,compacted_into_summary_id::text,is_compacted,created_at`, id, excluded))
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, notFound("Сообщение не найдено.")
	}
	return value, err
}
func (m *Memory) DeleteMessage(ctx context.Context, id int64) error {
	_, err := m.pool.Exec(ctx, `DELETE FROM agent_conversation_messages WHERE id=$1`, id)
	return err
}
func (m *Memory) ClearDialog(ctx context.Context, id int64) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `DELETE FROM agent_conversation_messages WHERE chat_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_context_summaries WHERE chat_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (m *Memory) Turn(ctx context.Context, id int64, content string, images []string, sandbox bool) (TurnResult, error) {
	if m.turner == nil {
		return TurnResult{}, &workout.Error{Status: http.StatusServiceUnavailable, Message: "Agent недоступен (OpenRouter не настроен)."}
	}
	return m.turner.Turn(ctx, id, content, images, sandbox)
}
func (m *Memory) Compact(ctx context.Context, chatID int64, keepRecent int) (CompactResult, error) {
	if m.compactor == nil {
		reason := "unavailable"
		return CompactResult{Reason: &reason}, nil
	}
	return m.compactor.Compact(ctx, chatID, keepRecent)
}

func (m *Memory) summaries(ctx context.Context, id int64) ([]Summary, error) {
	rows, err := m.pool.Query(ctx, `SELECT id::text,sequence,summary_text,covers_message_id_from,covers_message_id_to,source_message_count,model,tokens_before,tokens_after,created_at FROM agent_context_summaries WHERE chat_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Summary{}
	for rows.Next() {
		var v Summary
		if err := rows.Scan(&v.ID, &v.Sequence, &v.SummaryText, &v.CoversFrom, &v.CoversTo, &v.SourceCount, &v.Model, &v.TokensBefore, &v.TokensAfter, &v.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (m *Memory) facts(ctx context.Context, id int64) ([]Fact, error) {
	rows, err := m.pool.Query(ctx, `SELECT id::text,chat_id,content,confidence,created_at,updated_at FROM agent_user_facts WHERE chat_id=$1 ORDER BY updated_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Fact{}
	for rows.Next() {
		v, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (m *Memory) CreateTestChat(ctx context.Context, title string) (TestChat, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 120 {
		return TestChat{}, badRequest("Название чата не может быть пустым.")
	}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return TestChat{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var value TestChat
	err = tx.QueryRow(ctx, `INSERT INTO agent_test_chats(id,memory_chat_id,title) VALUES(gen_random_uuid(),nextval('agent_test_chat_memory_id_seq'),$1) RETURNING id::text,memory_chat_id,title,created_at,updated_at`, title).Scan(&value.ID, &value.MemoryChatID, &value.Title, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return TestChat{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_test_sandbox_states(memory_chat_id,state_json) VALUES($1,'{}')`, value.MemoryChatID); err != nil {
		return TestChat{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TestChat{}, err
	}
	value.Sandboxed = true
	return value, nil
}
func (m *Memory) ListTestChats(ctx context.Context) ([]TestChat, error) {
	rows, err := m.pool.Query(ctx, `SELECT c.id::text,c.memory_chat_id,c.title,(SELECT count(*) FROM agent_conversation_messages m WHERE m.chat_id=c.memory_chat_id),c.created_at,c.updated_at FROM agent_test_chats c ORDER BY c.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []TestChat{}
	for rows.Next() {
		var v TestChat
		if err := rows.Scan(&v.ID, &v.MemoryChatID, &v.Title, &v.MessageCount, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Sandboxed = true
		result = append(result, v)
	}
	return result, rows.Err()
}
func (m *Memory) TestChat(ctx context.Context, id string) (TestChat, error) {
	var v TestChat
	err := m.pool.QueryRow(ctx, `SELECT c.id::text,c.memory_chat_id,c.title,(SELECT count(*) FROM agent_conversation_messages m WHERE m.chat_id=c.memory_chat_id),c.created_at,c.updated_at FROM agent_test_chats c WHERE c.id=$1::uuid`, id).Scan(&v.ID, &v.MemoryChatID, &v.Title, &v.MessageCount, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestChat{}, notFound("Тестовый чат не найден.")
	}
	v.Sandboxed = true
	return v, err
}
func (m *Memory) RenameTestChat(ctx context.Context, id, title string) (TestChat, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 120 {
		return TestChat{}, badRequest("Название чата не может быть пустым.")
	}
	_, err := m.pool.Exec(ctx, `UPDATE agent_test_chats SET title=$2,updated_at=now() WHERE id=$1::uuid`, id, title)
	if err != nil {
		return TestChat{}, err
	}
	return m.TestChat(ctx, id)
}
func (m *Memory) DeleteTestChat(ctx context.Context, id string) error {
	chat, err := m.TestChat(ctx, id)
	if err != nil {
		return err
	}
	if err := m.ClearDialog(ctx, chat.MemoryChatID); err != nil {
		return err
	}
	_, err = m.pool.Exec(ctx, `DELETE FROM agent_test_chats WHERE id=$1::uuid`, id)
	return err
}
func (m *Memory) ClearTestChat(ctx context.Context, id string) error {
	chat, err := m.TestChat(ctx, id)
	if err != nil {
		return err
	}
	if err := m.ClearDialog(ctx, chat.MemoryChatID); err != nil {
		return err
	}
	_, err = m.pool.Exec(ctx, `UPDATE agent_test_sandbox_states SET state_json='{}',version=version+1,updated_at=now() WHERE memory_chat_id=$1`, chat.MemoryChatID)
	return err
}

func normalizeImages(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
func nilIfEmpty[T any](values []T) []T {
	if len(values) == 0 {
		return nil
	}
	return values
}
func badRequest(message string) error {
	return &workout.Error{Status: http.StatusBadRequest, Message: message}
}
func notFound(message string) error {
	return &workout.Error{Status: http.StatusNotFound, Message: message}
}
