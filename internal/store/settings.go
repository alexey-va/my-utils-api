package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SettingsStore struct {
	pool *pgxpool.Pool
}

func NewSettingsStore(pool *pgxpool.Pool) *SettingsStore {
	return &SettingsStore{pool: pool}
}

func (s *SettingsStore) EnsureDefinitions(ctx context.Context, definitions []settings.Definition) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin settings seed: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for _, definition := range definitions {
		tags, marshalErr := json.Marshal(definition.Tags)
		if marshalErr != nil {
			return fmt.Errorf("marshal tags for %s: %w", definition.Key, marshalErr)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_settings (key, value, tags)
			VALUES ($1, $2::jsonb, $3::jsonb)
			ON CONFLICT (key) DO NOTHING
		`, definition.Key, string(definition.Default), string(tags)); err != nil {
			return fmt.Errorf("seed runtime setting %s: %w", definition.Key, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE app_settings SET tags = $2::jsonb
			WHERE key = $1 AND tags IS DISTINCT FROM $2::jsonb
		`, definition.Key, string(tags)); err != nil {
			return fmt.Errorf("sync tags for runtime setting %s: %w", definition.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit settings seed: %w", err)
	}
	return nil
}

func (s *SettingsStore) Load(ctx context.Context) (map[string]settings.StoredSetting, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, value::text, tags::text, updated_at, updated_by
		FROM app_settings
	`)
	if err != nil {
		return nil, fmt.Errorf("query runtime settings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]settings.StoredSetting)
	for rows.Next() {
		var key, rawValue, rawTags string
		var updatedAt time.Time
		var updatedBy *string
		if err := rows.Scan(&key, &rawValue, &rawTags, &updatedAt, &updatedBy); err != nil {
			return nil, fmt.Errorf("scan runtime setting: %w", err)
		}
		var tags []string
		if err := json.Unmarshal([]byte(rawTags), &tags); err != nil {
			return nil, fmt.Errorf("decode runtime setting tags for %s: %w", key, err)
		}
		at := updatedAt
		result[key] = settings.StoredSetting{
			Key: key, Value: json.RawMessage(rawValue), Tags: tags,
			UpdatedAt: &at, UpdatedBy: updatedBy,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime settings: %w", err)
	}
	return result, nil
}

func (s *SettingsStore) Update(ctx context.Context, key string, value json.RawMessage, updatedBy string) (settings.StoredSetting, error) {
	var rawValue, rawTags string
	var updatedAt time.Time
	var storedBy *string
	err := s.pool.QueryRow(ctx, `
		UPDATE app_settings
		SET value = $2::jsonb, updated_at = now(), updated_by = NULLIF($3, '')
		WHERE key = $1
		RETURNING value::text, tags::text, updated_at, updated_by
	`, key, string(value), updatedBy).Scan(&rawValue, &rawTags, &updatedAt, &storedBy)
	if err != nil {
		return settings.StoredSetting{}, fmt.Errorf("update runtime setting %s: %w", key, err)
	}
	var tags []string
	if err := json.Unmarshal([]byte(rawTags), &tags); err != nil {
		return settings.StoredSetting{}, fmt.Errorf("decode updated tags for %s: %w", key, err)
	}
	return settings.StoredSetting{
		Key: key, Value: json.RawMessage(rawValue), Tags: tags,
		UpdatedAt: &updatedAt, UpdatedBy: storedBy,
	}, nil
}
