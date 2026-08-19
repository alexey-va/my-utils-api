package clientevents

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

const maxBodyLength = 64 * 1024

var (
	typePattern       = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	tokenPattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,160}$`)
	fieldStatePattern = regexp.MustCompile(`^(empty|nonempty|checked|unchecked|redacted)$`)
	ipPattern         = regexp.MustCompile(`^[0-9a-fA-F:.]{2,64}$`)
)

type requestBatch struct {
	ClientApp *string        `json:"clientApp"`
	Events    []requestEvent `json:"events"`
}
type requestEvent struct {
	EventID             *string `json:"eventId"`
	ClientID            *string `json:"clientId"`
	SessionID           *string `json:"sessionId"`
	PageViewID          *string `json:"pageViewId"`
	OccurredAt          *string `json:"occurredAt"`
	Type                *string `json:"type"`
	Page                *string `json:"page"`
	UIMode              *string `json:"uiMode"`
	TargetTag           *string `json:"targetTag"`
	TargetKey           *string `json:"targetKey"`
	TargetType          *string `json:"targetType"`
	Detail              *string `json:"detail"`
	ViewportWidth       *int    `json:"viewportWidth"`
	ViewportHeight      *int    `json:"viewportHeight"`
	ScreenWidth         *int    `json:"screenWidth"`
	ScreenHeight        *int    `json:"screenHeight"`
	Sequence            *int    `json:"sequence"`
	ElapsedMS           *int64  `json:"elapsedMs"`
	SincePreviousMS     *int64  `json:"sincePreviousMs"`
	DurationMS          *int64  `json:"durationMs"`
	Changed             *bool   `json:"changed"`
	Webdriver           *bool   `json:"webdriver"`
	FieldState          *string `json:"fieldState"`
	Language            *string `json:"language"`
	Platform            *string `json:"platform"`
	HardwareConcurrency *int    `json:"hardwareConcurrency"`
	MaxTouchPoints      *int    `json:"maxTouchPoints"`
}

type Batch struct {
	ClientApp string
	Events    []Event
}
type Event struct {
	EventID, ClientID, SessionID, PageViewID *string
	OccurredAt                               *string
	Type, Page                               string
	UIMode, TargetTag, TargetKey, TargetType *string
	Detail                                   *string
	ViewportWidth, ViewportHeight            *int
	ScreenWidth, ScreenHeight, Sequence      *int
	ElapsedMS, SincePreviousMS, DurationMS   *int64
	Changed, Webdriver                       *bool
	FieldState, Language, Platform           *string
	HardwareConcurrency, MaxTouchPoints      *int
}

type RequestContext struct {
	Origin, IPAddress, UserAgent, AcceptLanguage *string
	SecCHUA, SecCHUAPlatform, SecCHUAMobile      *string
}

func Parse(body []byte) *Batch {
	if len(body) == 0 || len(body) > maxBodyLength || strings.TrimSpace(string(body)) == "" {
		return nil
	}
	var request requestBatch
	if json.Unmarshal(body, &request) != nil {
		return nil
	}
	clientApp := "route-planner"
	if request.ClientApp != nil && *request.ClientApp != "" {
		if *request.ClientApp != "route-planner" && *request.ClientApp != "my-utils" {
			return nil
		}
		clientApp = *request.ClientApp
	}
	result := &Batch{ClientApp: clientApp, Events: []Event{}}
	limit := min(len(request.Events), 25)
	for _, raw := range request.Events[:limit] {
		if event := sanitizeEvent(raw); event != nil {
			result.Events = append(result.Events, *event)
		}
	}
	return result
}

func sanitizeEvent(raw requestEvent) *Event {
	eventType := safeText(raw.Type, 64)
	if eventType == nil || !typePattern.MatchString(*eventType) {
		return nil
	}
	page := "/"
	if value := safeText(raw.Page, 120); value != nil {
		candidate := strings.SplitN(*value, "?", 2)[0]
		if strings.HasPrefix(candidate, "/") {
			page = candidate
		}
	}
	return &Event{
		EventID: safeToken(raw.EventID), ClientID: safeToken(raw.ClientID), SessionID: safeToken(raw.SessionID), PageViewID: safeToken(raw.PageViewID),
		OccurredAt: safeInstant(raw.OccurredAt), Type: *eventType, Page: page,
		UIMode: safeText(raw.UIMode, 40), TargetTag: safeText(raw.TargetTag, 40), TargetKey: safeText(raw.TargetKey, 160), TargetType: safeText(raw.TargetType, 40), Detail: safeText(raw.Detail, 160),
		ViewportWidth: safeInt(raw.ViewportWidth, 0, 20_000), ViewportHeight: safeInt(raw.ViewportHeight, 0, 20_000), ScreenWidth: safeInt(raw.ScreenWidth, 0, 20_000), ScreenHeight: safeInt(raw.ScreenHeight, 0, 20_000),
		Sequence: safeInt(raw.Sequence, 0, 1_000_000), ElapsedMS: safeInt64(raw.ElapsedMS, 0, 86_400_000), SincePreviousMS: safeInt64(raw.SincePreviousMS, 0, 86_400_000), DurationMS: safeInt64(raw.DurationMS, 0, 86_400_000),
		Changed: raw.Changed, FieldState: safePattern(raw.FieldState, 40, fieldStatePattern), Webdriver: raw.Webdriver,
		Language: safeText(raw.Language, 40), Platform: safeText(raw.Platform, 40), HardwareConcurrency: safeInt(raw.HardwareConcurrency, 0, 1024), MaxTouchPoints: safeInt(raw.MaxTouchPoints, 0, 1024),
	}
}

func LogBatch(ctx context.Context, logger *slog.Logger, body []byte, requestContext RequestContext) int {
	batch := Parse(body)
	if batch == nil {
		return 0
	}
	context := sanitizeContext(requestContext)
	for _, event := range batch.Events {
		logger.LogAttrs(ctx, slog.LevelInfo, "Client event",
			slog.String("event_type", "client_event"), slog.String("client_app", batch.ClientApp), slog.String("client_event_type", event.Type),
			anyAttr("client_event_id", event.EventID), anyAttr("client_id", event.ClientID), anyAttr("client_session_id", event.SessionID), anyAttr("client_page_view_id", event.PageViewID),
			anyAttr("client_occurred_at", event.OccurredAt), anyAttr("client_sequence", event.Sequence), anyAttr("client_elapsed_ms", event.ElapsedMS), anyAttr("client_since_previous_ms", event.SincePreviousMS),
			slog.String("client_page", event.Page), anyAttr("client_ui_mode", event.UIMode), anyAttr("client_target_tag", event.TargetTag), anyAttr("client_target_key", event.TargetKey), anyAttr("client_target_type", event.TargetType), anyAttr("client_detail", event.Detail),
			anyAttr("client_viewport_width", event.ViewportWidth), anyAttr("client_viewport_height", event.ViewportHeight), anyAttr("client_screen_width", event.ScreenWidth), anyAttr("client_screen_height", event.ScreenHeight), anyAttr("client_duration_ms", event.DurationMS),
			anyAttr("client_changed", event.Changed), anyAttr("client_field_state", event.FieldState), anyAttr("client_webdriver", event.Webdriver), anyAttr("client_language", event.Language), anyAttr("client_platform", event.Platform),
			anyAttr("client_hardware_concurrency", event.HardwareConcurrency), anyAttr("client_max_touch_points", event.MaxTouchPoints),
			anyAttr("client_ip", context.IPAddress), anyAttr("client_user_agent", context.UserAgent), anyAttr("client_accept_language", context.AcceptLanguage), anyAttr("client_sec_ch_ua", context.SecCHUA), anyAttr("client_sec_ch_ua_platform", context.SecCHUAPlatform), anyAttr("client_sec_ch_ua_mobile", context.SecCHUAMobile), anyAttr("client_origin", context.Origin),
		)
	}
	return len(batch.Events)
}

func sanitizeContext(value RequestContext) RequestContext {
	value.Origin = safeText(value.Origin, 200)
	value.IPAddress = safePattern(value.IPAddress, 64, ipPattern)
	value.UserAgent = safeText(value.UserAgent, 512)
	value.AcceptLanguage = safeText(value.AcceptLanguage, 512)
	value.SecCHUA = safeText(value.SecCHUA, 512)
	value.SecCHUAPlatform = safeText(value.SecCHUAPlatform, 512)
	value.SecCHUAMobile = safeText(value.SecCHUAMobile, 512)
	return value
}

func safeText(value *string, maximum int) *string {
	if value == nil {
		return nil
	}
	normalized := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, *value)
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return nil
	}
	runes := []rune(normalized)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	result := string(runes)
	return &result
}
func safeToken(value *string) *string {
	result := safeText(value, 160)
	if result != nil && tokenPattern.MatchString(*result) {
		return result
	}
	return nil
}
func safePattern(value *string, maximum int, pattern *regexp.Regexp) *string {
	result := safeText(value, maximum)
	if result != nil && pattern.MatchString(*result) {
		return result
	}
	return nil
}
func safeInstant(value *string) *string {
	if value == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil
	}
	result := parsed.UTC().Format(time.RFC3339Nano)
	return &result
}
func safeInt(value *int, minimum, maximum int) *int {
	if value != nil && *value >= minimum && *value <= maximum {
		return value
	}
	return nil
}
func safeInt64(value *int64, minimum, maximum int64) *int64 {
	if value != nil && *value >= minimum && *value <= maximum {
		return value
	}
	return nil
}
func anyAttr(key string, value any) slog.Attr { return slog.Any(key, dereference(value)) }
func dereference(value any) any {
	switch typed := value.(type) {
	case *string:
		if typed != nil {
			return *typed
		}
	case *int:
		if typed != nil {
			return *typed
		}
	case *int64:
		if typed != nil {
			return *typed
		}
	case *bool:
		if typed != nil {
			return *typed
		}
	}
	return nil
}
