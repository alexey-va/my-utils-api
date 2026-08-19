package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type ValueType string

const (
	TypeBoolean ValueType = "BOOLEAN"
	TypeInt     ValueType = "INT"
	TypeString  ValueType = "STRING"
)

type ApplyFunc func(context.Context) error

type Definition struct {
	Key         string
	Type        ValueType
	ObjectType  *string
	Description string
	Tags        []string
	Default     json.RawMessage
	Editor      string
	Normalize   func(json.RawMessage) (json.RawMessage, error)
	OnApplied   ApplyFunc
}

func Boolean(key, description string, tags []string, fallback bool, onApplied ApplyFunc) Definition {
	raw := json.RawMessage("false")
	if fallback {
		raw = json.RawMessage("true")
	}
	return Definition{
		Key: key, Type: TypeBoolean, Description: description, Tags: cloneStrings(tags), Default: raw, Editor: "DEFAULT", OnApplied: onApplied,
		Normalize: func(input json.RawMessage) (json.RawMessage, error) {
			var value bool
			if err := json.Unmarshal(input, &value); err == nil {
				return json.Marshal(value)
			}
			var text string
			if err := json.Unmarshal(input, &text); err == nil {
				parsed, parseErr := strconv.ParseBool(strings.TrimSpace(text))
				if parseErr == nil {
					return json.Marshal(parsed)
				}
			}
			return nil, fmt.Errorf("%s expects a boolean", key)
		},
	}
}

func Int(key, description string, tags []string, fallback, minimum, maximum int, onApplied ApplyFunc) Definition {
	defaultRaw, _ := json.Marshal(fallback)
	return Definition{
		Key: key, Type: TypeInt, Description: description, Tags: cloneStrings(tags), Default: defaultRaw, Editor: "DEFAULT", OnApplied: onApplied,
		Normalize: func(input json.RawMessage) (json.RawMessage, error) {
			var value int
			decoder := json.NewDecoder(bytes.NewReader(input))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&value); err != nil {
				var text string
				if textErr := json.Unmarshal(input, &text); textErr != nil {
					return nil, fmt.Errorf("%s expects an integer", key)
				}
				parsed, parseErr := strconv.Atoi(strings.TrimSpace(text))
				if parseErr != nil {
					return nil, fmt.Errorf("%s expects an integer", key)
				}
				value = parsed
			}
			if value < minimum || value > maximum {
				return nil, fmt.Errorf("%s must be in %d..%d", key, minimum, maximum)
			}
			return json.Marshal(value)
		},
	}
}

func String(key, description string, tags []string, fallback string, validate func(string) bool, onApplied ApplyFunc) Definition {
	defaultRaw, _ := json.Marshal(fallback)
	return Definition{
		Key: key, Type: TypeString, Description: description, Tags: cloneStrings(tags), Default: defaultRaw, Editor: "DEFAULT", OnApplied: onApplied,
		Normalize: func(input json.RawMessage) (json.RawMessage, error) {
			var value string
			if err := json.Unmarshal(input, &value); err != nil {
				return nil, fmt.Errorf("%s expects a string", key)
			}
			if validate != nil && !validate(value) {
				return nil, fmt.Errorf("invalid value for %s", key)
			}
			return json.Marshal(value)
		},
	}
}

type Catalog struct {
	definitions []Definition
	byKey       map[string]Definition
}

func NewCatalog(definitions []Definition) Catalog {
	copied := append([]Definition(nil), definitions...)
	sort.Slice(copied, func(i, j int) bool { return copied[i].Key < copied[j].Key })
	byKey := make(map[string]Definition, len(copied))
	for _, definition := range copied {
		if definition.Key == "" || definition.Normalize == nil {
			panic("runtime setting definition is incomplete")
		}
		if _, exists := byKey[definition.Key]; exists {
			panic("duplicate runtime setting: " + definition.Key)
		}
		definition.Default = cloneRaw(definition.Default)
		definition.Tags = cloneStrings(definition.Tags)
		byKey[definition.Key] = definition
	}
	return Catalog{definitions: copied, byKey: byKey}
}

func (c Catalog) Definitions() []Definition {
	return append([]Definition(nil), c.definitions...)
}

type StoredSetting struct {
	Key       string
	Value     json.RawMessage
	Tags      []string
	UpdatedAt *time.Time
	UpdatedBy *string
}

type Store interface {
	EnsureDefinitions(context.Context, []Definition) error
	Load(context.Context) (map[string]StoredSetting, error)
	Update(context.Context, string, json.RawMessage, string) (StoredSetting, error)
}

type View struct {
	Key          string          `json:"key"`
	Type         ValueType       `json:"type"`
	ObjectType   *string         `json:"objectType"`
	Description  string          `json:"description"`
	Tags         []string        `json:"tags"`
	Value        json.RawMessage `json:"value"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	Editor       string          `json:"editor"`
	UpdatedAt    *time.Time      `json:"updatedAt"`
	UpdatedBy    *string         `json:"updatedBy"`
}

type snapshot struct {
	values map[string]json.RawMessage
	stored map[string]StoredSetting
}

type Service struct {
	catalog Catalog
	store   Store
	state   atomic.Pointer[snapshot]
}

func NewService(catalog Catalog, store Store) *Service {
	service := &Service{catalog: catalog, store: store}
	service.state.Store(&snapshot{values: map[string]json.RawMessage{}, stored: map[string]StoredSetting{}})
	return service
}

func (s *Service) Name() string { return "runtime-settings" }

func (s *Service) Warm(ctx context.Context) error {
	if err := s.store.EnsureDefinitions(ctx, s.catalog.Definitions()); err != nil {
		return fmt.Errorf("ensure runtime setting definitions: %w", err)
	}
	return s.Reload(ctx)
}

func (s *Service) Reload(ctx context.Context) error {
	stored, err := s.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("load runtime settings: %w", err)
	}
	values := make(map[string]json.RawMessage, len(s.catalog.definitions))
	for _, definition := range s.catalog.definitions {
		raw := definition.Default
		if setting, ok := stored[definition.Key]; ok {
			raw = setting.Value
		}
		normalized, normalizeErr := definition.Normalize(raw)
		if normalizeErr != nil {
			slog.WarnContext(ctx, "invalid stored runtime setting; using default", "key", definition.Key, "error", normalizeErr)
			normalized = cloneRaw(definition.Default)
		}
		values[definition.Key] = normalized
	}
	s.state.Store(&snapshot{values: values, stored: cloneStoredMap(stored)})
	return nil
}

func (s *Service) RunRefresh(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reload(ctx); err != nil {
				slog.WarnContext(ctx, "runtime settings refresh failed", "error", err)
			}
		}
	}
}

func (s *Service) Update(ctx context.Context, key string, input json.RawMessage, updatedBy string) (View, error) {
	definition, ok := s.catalog.byKey[key]
	if !ok {
		return View{}, ErrUnknownSetting
	}
	normalized, err := definition.Normalize(input)
	if err != nil {
		return View{}, err
	}
	stored, err := s.store.Update(ctx, key, normalized, updatedBy)
	if err != nil {
		return View{}, fmt.Errorf("persist runtime setting: %w", err)
	}
	previous := s.state.Load()
	values := cloneRawMap(previous.values)
	storedMap := cloneStoredMap(previous.stored)
	values[key] = cloneRaw(normalized)
	storedMap[key] = stored
	s.state.Store(&snapshot{values: values, stored: storedMap})
	if definition.OnApplied != nil {
		if err := definition.OnApplied(ctx); err != nil {
			return View{}, fmt.Errorf("apply runtime setting %s: %w", key, err)
		}
	}
	return viewOf(definition, normalized, stored), nil
}

func (s *Service) List() []View {
	current := s.state.Load()
	result := make([]View, 0, len(s.catalog.definitions))
	for _, definition := range s.catalog.definitions {
		stored := current.stored[definition.Key]
		result = append(result, viewOf(definition, current.values[definition.Key], stored))
	}
	return result
}

func (s *Service) Bool(key string) bool {
	var value bool
	_ = json.Unmarshal(s.raw(key), &value)
	return value
}

func (s *Service) Int(key string) int {
	var value int
	_ = json.Unmarshal(s.raw(key), &value)
	return value
}

func (s *Service) String(key string) string {
	var value string
	_ = json.Unmarshal(s.raw(key), &value)
	return value
}

func (s *Service) raw(key string) json.RawMessage {
	current := s.state.Load()
	if value, ok := current.values[key]; ok {
		return value
	}
	return nil
}

func viewOf(definition Definition, value json.RawMessage, stored StoredSetting) View {
	tags := definition.Tags
	if len(stored.Tags) > 0 {
		tags = stored.Tags
	}
	return View{
		Key: definition.Key, Type: definition.Type, ObjectType: definition.ObjectType,
		Description: definition.Description, Tags: cloneStrings(tags), Value: cloneRaw(value),
		DefaultValue: cloneRaw(definition.Default), Editor: definition.Editor,
		UpdatedAt: stored.UpdatedAt, UpdatedBy: stored.UpdatedBy,
	}
}

var ErrUnknownSetting = errors.New("unknown runtime setting")

func cloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneRawMap(values map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		result[key] = cloneRaw(value)
	}
	return result
}

func cloneStoredMap(values map[string]StoredSetting) map[string]StoredSetting {
	result := make(map[string]StoredSetting, len(values))
	for key, value := range values {
		value.Value = cloneRaw(value.Value)
		value.Tags = cloneStrings(value.Tags)
		result[key] = value
	}
	return result
}
