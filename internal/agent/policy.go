package agent

import (
	"encoding/json"
	"fmt"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"io"
	"math"
	"strings"
)

// Interpret once, validate the entire plan, execute only its exact actions.
// Natural language intent is not reinterpreted by a second lexical classifier.
type turnDecision struct {
	Mode    string          `json:"mode"`
	Reply   string          `json:"reply"`
	Actions []plannedAction `json:"actions"`
}
type plannedAction struct {
	Tool         string         `json:"tool"`
	Arguments    map[string]any `json:"arguments"`
	RequestQuote string         `json:"request_quote"`
	DataQuote    string         `json:"data_quote,omitempty"`
}

func NormalizeToolName(value string) string {
	var out strings.Builder
	for i, c := range value {
		if i > 0 && c >= 'A' && c <= 'Z' {
			out.WriteByte('_')
		}
		out.WriteRune(c)
	}
	return strings.ToLower(out.String())
}
func isReadTool(name string) bool {
	switch NormalizeToolName(name) {
	case "list_exercises", "get_progress", "get_days", "get_body_weight", "get_conversation_history":
		return true
	}
	return false
}
func decisionTool(catalog []openrouter.Tool) openrouter.Tool {
	variants := make([]any, 0, len(catalog))
	for _, tool := range catalog {
		variants = append(variants, map[string]any{"type": "object", "additionalProperties": false, "required": []string{"tool", "arguments", "request_quote"}, "properties": map[string]any{
			"tool":          map[string]any{"type": "string", "enum": []string{tool.Function.Name}, "description": tool.Function.Description},
			"arguments":     tool.Function.Parameters,
			"data_quote":    map[string]any{"type": "string", "description": "Для log_workout: дословный фрагмент пользовательской реплики с числами РОВНО ОДНОГО упражнения. Можно из предыдущего незавершённого запроса. Обязателен для log_workout. Для copy_workout с новым weight_kg — точная фраза пользователя с одним новым весом. Не цитируй модель или снимок."},
			"request_quote": map[string]any{"type": "string", "description": "Дословный фрагмент ТЕКУЩЕГО сообщения пользователя, запрашивающий действие. Для чтения пустая строка."},
		}})
	}
	return openrouter.Tool{Type: "function", Function: openrouter.ToolFunction{Name: "resolve_turn", Description: "Пойми текущую реплику в контексте диалога. Верни ответ ИЛИ точный план действий, ещё не результат выполнения.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"mode", "reply", "actions"}, "properties": map[string]any{
			"mode":    map[string]any{"type": "string", "enum": []string{"read", "write", "clarify"}},
			"reply":   map[string]any{"type": "string", "description": "Ответ при read/clarify без действий; иначе пустая строка. Не обещай успех до выполнения."},
			"actions": map[string]any{"type": "array", "maxItems": 20, "items": map[string]any{"oneOf": variants}},
		},
	}}}
}
func parseDecision(message openrouter.Message, userText string, catalog []openrouter.Tool, readOnly bool) (turnDecision, error) {
	var d turnDecision
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "resolve_turn" {
		return d, fmt.Errorf("ожидался один resolve_turn")
	}
	dec := json.NewDecoder(strings.NewReader(message.ToolCalls[0].Function.Arguments))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return d, fmt.Errorf("лишние данные после решения")
	}
	if d.Mode != "read" && d.Mode != "write" && d.Mode != "clarify" {
		return d, fmt.Errorf("неизвестный mode")
	}
	if len(d.Actions) > 20 {
		return d, fmt.Errorf("не более 20 действий")
	}
	if len(d.Actions) == 0 {
		if d.Mode == "write" || strings.TrimSpace(d.Reply) == "" {
			return d, fmt.Errorf("нужен ответ или действие")
		}
		return d, nil
	}
	if d.Mode == "clarify" || strings.TrimSpace(d.Reply) != "" {
		return d, fmt.Errorf("ответ и действия должны быть раздельными")
	}
	defs := map[string]openrouter.Tool{}
	for _, t := range catalog {
		defs[t.Function.Name] = t
	}
	seen, targets := map[string]bool{}, map[string]bool{}
	hasExternal, hasDatabase := false, false
	for i := range d.Actions {
		a := &d.Actions[i]
		a.Tool = NormalizeToolName(a.Tool)
		if isExternalTool(a.Tool) {
			hasExternal = true
		} else {
			hasDatabase = true
		}
		def, ok := defs[a.Tool]
		if !ok {
			return d, fmt.Errorf("неизвестный инструмент %q", a.Tool)
		}
		if !isReadTool(a.Tool) {
			if readOnly || d.Mode != "write" {
				return d, fmt.Errorf("режим чтения запрещает %s", a.Tool)
			}
			q := strings.TrimSpace(a.RequestQuote)
			if q == "" || !strings.Contains(userText, q) {
				return d, fmt.Errorf("%s: нет дословного основания в текущем запросе", a.Tool)
			}
		}
		if err := validateArguments(a.Arguments, def.Function.Parameters); err != nil {
			return d, fmt.Errorf("%s: %w", a.Tool, err)
		}
		key := actionKey(a.Tool, a.Arguments)
		if seen[key] {
			return d, fmt.Errorf("действие %s повторяется", a.Tool)
		}
		seen[key] = true
		// Correction is one upsert. A delete+write plan risks losing the old row.
		switch a.Tool {
		case "log_workout", "copy_workout", "delete_workout":
			date, _ := a.Arguments["date"].(string)
			if date == "" {
				date, _ = a.Arguments["performed_on"].(string)
			}
			name, _ := a.Arguments["exercise_name"].(string)
			identity := name
			if id, _ := a.Arguments["exercise_id"].(string); id != "" {
				identity = id
			}
			target := strings.ToLower(strings.TrimSpace(identity)) + ":" + date
			if targets[target] {
				return d, fmt.Errorf("несколько изменений одной записи %s; используй один upsert", name)
			}
			targets[target] = true
		}
	}
	if hasExternal && hasDatabase {
		return d, fmt.Errorf("раздели чтение/изменение дневника и отправку/напоминания: внешние действия не входят в транзакцию дневника")
	}
	return d, nil
}
func actionKey(name string, args map[string]any) string {
	raw, _ := json.Marshal(args)
	return NormalizeToolName(name) + ":" + string(raw)
}
func validateArguments(args map[string]any, schema map[string]any) error {
	if args == nil {
		return fmt.Errorf("arguments должен быть объектом")
	}
	props, _ := schema["properties"].(map[string]any)
	if required, ok := schema["required"].([]string); ok {
		for _, key := range required {
			if _, ok := args[key]; !ok {
				return fmt.Errorf("обязательное поле %s отсутствует", key)
			}
		}
	}
	for key, value := range args {
		raw, ok := props[key]
		if !ok {
			return fmt.Errorf("неизвестное поле %s", key)
		}
		p := raw.(map[string]any)
		switch p["type"] {
		case "string":
			s, ok := value.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return fmt.Errorf("%s должен быть непустой строкой", key)
			}
			if values, ok := p["enum"].([]string); ok {
				found := false
				for _, v := range values {
					found = found || s == v
				}
				if !found {
					return fmt.Errorf("недопустимое значение %s", key)
				}
			}
		case "integer", "number":
			n, ok := value.(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || (p["type"] == "integer" && math.Trunc(n) != n) {
				return fmt.Errorf("%s должен быть числом нужного типа", key)
			}
		}
	}
	return nil
}
