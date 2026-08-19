package settings

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestWarmSeedsDefinitionsAndLoadsValidatedSnapshot(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog([]Definition{
		Int("hour", "Hour", []string{"temporal"}, 20, 0, 23, nil),
		Int("minute", "Minute", []string{"temporal"}, 0, 0, 59, nil),
		String("zone", "Zone", []string{"temporal"}, "Europe/Moscow", func(value string) bool { return value != "" }, nil),
	})
	store := &memoryStore{values: map[string]StoredSetting{
		"hour":   {Key: "hour", Value: json.RawMessage("22"), Tags: []string{"old"}},
		"minute": {Key: "minute", Value: json.RawMessage("99")},
	}}
	service := NewService(catalog, store)

	if service.Name() != "runtime-settings" {
		t.Errorf("Name() = %q", service.Name())
	}
	if err := service.Warm(context.Background()); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	if got := service.Int("hour"); got != 22 {
		t.Errorf("hour = %d, want stored value 22", got)
	}
	if got := service.Int("minute"); got != 0 {
		t.Errorf("minute = %d, want validated default 0", got)
	}
	if got := service.String("zone"); got != "Europe/Moscow" {
		t.Errorf("zone = %q, want seeded default", got)
	}
	if got := store.values["hour"].Tags; !reflect.DeepEqual(got, []string{"temporal"}) {
		t.Errorf("synced tags = %#v", got)
	}
}

func TestUpdateValidatesPersistsAndPublishesOneSnapshot(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog([]Definition{
		Int("hour", "Hour", nil, 20, 0, 23, nil),
	})
	store := &memoryStore{values: map[string]StoredSetting{}}
	service := NewService(catalog, store)
	if err := service.Warm(context.Background()); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}

	view, err := service.Update(context.Background(), "hour", json.RawMessage("23"), "admin")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if service.Int("hour") != 23 || string(store.values["hour"].Value) != "23" {
		t.Fatalf("updated cache/store = %d/%s", service.Int("hour"), store.values["hour"].Value)
	}
	if view.UpdatedBy == nil || *view.UpdatedBy != "admin" || string(view.Value) != "23" {
		t.Errorf("view = %#v", view)
	}

	_, err = service.Update(context.Background(), "hour", json.RawMessage("24"), "admin")
	if err == nil {
		t.Fatal("Update(24) succeeded, want range error")
	}
	if service.Int("hour") != 23 || string(store.values["hour"].Value) != "23" {
		t.Fatal("invalid update changed cache or store")
	}
}

func TestUpdateRunsSideEffectAfterPublishingValue(t *testing.T) {
	t.Parallel()

	var observed bool
	var service *Service
	definition := Boolean("enabled", "Enabled", nil, false, func(context.Context) error {
		observed = service.Bool("enabled")
		return nil
	})
	store := &memoryStore{values: map[string]StoredSetting{}}
	service = NewService(NewCatalog([]Definition{definition}), store)
	if err := service.Warm(context.Background()); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}

	if _, err := service.Update(context.Background(), "enabled", json.RawMessage("true"), "admin"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !observed {
		t.Fatal("side effect did not observe the newly published value")
	}
}

type memoryStore struct {
	values map[string]StoredSetting
}

func (s *memoryStore) EnsureDefinitions(_ context.Context, definitions []Definition) error {
	if s.values == nil {
		s.values = make(map[string]StoredSetting)
	}
	for _, definition := range definitions {
		value, ok := s.values[definition.Key]
		if !ok {
			s.values[definition.Key] = StoredSetting{Key: definition.Key, Value: cloneRaw(definition.Default), Tags: append([]string(nil), definition.Tags...)}
			continue
		}
		value.Tags = append([]string(nil), definition.Tags...)
		s.values[definition.Key] = value
	}
	return nil
}

func (s *memoryStore) Load(context.Context) (map[string]StoredSetting, error) {
	result := make(map[string]StoredSetting, len(s.values))
	for key, value := range s.values {
		value.Value = cloneRaw(value.Value)
		result[key] = value
	}
	return result, nil
}

func (s *memoryStore) Update(_ context.Context, key string, value json.RawMessage, updatedBy string) (StoredSetting, error) {
	setting, ok := s.values[key]
	if !ok {
		return StoredSetting{}, errors.New("missing setting")
	}
	setting.Value = cloneRaw(value)
	setting.UpdatedBy = &updatedBy
	s.values[key] = setting
	return setting, nil
}
