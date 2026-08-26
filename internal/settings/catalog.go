package settings

import (
	_ "embed"
	"strings"
	"time"
)

const (
	TemporalReminderEnabled = "temporal.evening-reminder.enabled"
	TemporalReminderHour    = "temporal.evening-reminder.hour"
	TemporalReminderMinute  = "temporal.evening-reminder.minute"
	TemporalZoneID          = "temporal.zone-id"
	OpenRouterModel         = "openrouter.model"
	OpenRouterMaxTools      = "openrouter.max-tool-iterations"
	AgentRecentMessages     = "agent.memory.recent-messages"
	AgentRecentEntries      = "agent.context.recent-entries"
	AgentCalendarDays       = "agent.context.calendar-days"
	AgentProgressSessions   = "agent.context.progress-sessions"
	AgentCompactThreshold   = "agent.memory.compact-threshold-messages"
	AgentCompactModel       = "agent.memory.compact-model"
	OpenRouterRetryAttempts = "openrouter.retry.max-attempts"
	OpenRouterRetryDelayMS  = "openrouter.retry.initial-delay-ms"
	TelegramConversationTTL = "telegram.conversation-ttl-hours"
	AgentSystemPrompt       = "agent.system-prompt"
)

//go:embed agent_system_prompt.txt
var agentSystemPromptDefault string

//go:embed agent_required_rules.txt
var agentRequiredRules string

const agentRequiredRulesMarker = "## Обязательные runtime-инварианты (не переопределять)"

func EffectiveAgentSystemPrompt(configured string) string {
	configured = strings.TrimSpace(configured)
	if strings.Contains(configured, agentRequiredRulesMarker) {
		return configured
	}
	rules := strings.TrimSpace(agentRequiredRules)
	if configured == "" {
		return rules
	}
	return configured + "\n\n" + rules
}

func AppCatalog(reminderChanged ApplyFunc) Catalog {
	prompt := String(
		AgentSystemPrompt,
		"System prompt Telegram-агента (OpenRouter). Редактируется без перезапуска.",
		[]string{"agent", "telegram"},
		EffectiveAgentSystemPrompt(agentSystemPromptDefault),
		func(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 32_000 },
		nil,
	)
	prompt.Editor = "TEXTAREA"
	return NewCatalog([]Definition{
		Boolean(TemporalReminderEnabled, "Вечернее напоминание в Telegram, если дневник пуст (Temporal workflow).", []string{"temporal"}, false, reminderChanged),
		Int(TemporalReminderHour, "Час напоминания (0–23), часовой пояс temporal.zone-id.", []string{"temporal"}, 20, 0, 23, reminderChanged),
		Int(TemporalReminderMinute, "Минута напоминания (0–59).", []string{"temporal"}, 0, 0, 59, reminderChanged),
		String(TemporalZoneID, "Часовой пояс для напоминаний и снимка дневника (например Europe/Moscow).", []string{"temporal"}, "Europe/Moscow", validTimeZone, reminderChanged),
		String(OpenRouterModel, "Модель OpenRouter для Telegram-агента (provider/model-id или @preset/…).", []string{"agent"}, "@preset/deepseek", validModel, nil),
		Int(OpenRouterMaxTools, "Максимум итераций tool-calling за одно сообщение.", []string{"agent"}, 30, 1, 64, nil),
		Int(AgentRecentMessages, "Сколько последних сообщений диалога подставлять в запрос LLM (полная история хранится в Postgres).", []string{"agent"}, 10, 1, 50, nil),
		Int(AgentRecentEntries, "Сколько последних записей дневника включать в снимок контекста агента при каждом запросе.", []string{"agent"}, 30, 1, 100, nil),
		Int(AgentCalendarDays, "Сколько последних дней (календарь) включать в снимок контекста агента.", []string{"agent"}, 14, 1, 31, nil),
		Int(AgentProgressSessions, "Сколько последних сессий на упражнение включать в снимок (история прогресса).", []string{"agent"}, 4, 1, 12, nil),
		Int(AgentCompactThreshold, "После скольких compactable сообщений (минус recent tail) запускать авто-сжатие.", []string{"agent"}, 40, 5, 500, nil),
		String(AgentCompactModel, "Модель OpenRouter для сжатия диалога (пусто → openrouter.model).", []string{"agent"}, "", func(value string) bool { return len(value) <= 200 }, nil),
		Int(OpenRouterRetryAttempts, "Число попыток запроса к OpenRouter при сетевых/временных ошибках.", []string{"agent"}, 3, 1, 10, nil),
		Int(OpenRouterRetryDelayMS, "Начальная пауза перед повтором OpenRouter (мс), далее экспоненциальный backoff.", []string{"agent"}, 1_000, 100, 60_000, nil),
		Int(TelegramConversationTTL, "Устарело: история чата хранится в Postgres (agent.memory.recent-messages).", []string{"telegram"}, 48, 1, 24*30, nil),
		prompt,
	})
}

func validTimeZone(value string) bool {
	_, err := time.LoadLocation(value)
	return err == nil
}

func validModel(value string) bool {
	return len(value) >= 3 && len(value) <= 200 && (strings.Contains(value, "/") || strings.HasPrefix(value, "@preset/"))
}
