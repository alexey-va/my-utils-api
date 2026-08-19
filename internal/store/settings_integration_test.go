package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSettingsStoreSeedsSyncsLoadsAndUpdatesJSON(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	store := NewSettingsStore(pool)
	key := fmt.Sprintf("go.test.setting.%d", time.Now().UnixNano())
	definitions := []settings.Definition{
		settings.Int(key, "test", []string{"go", "test"}, 7, 1, 10, nil),
	}

	if err := store.EnsureDefinitions(ctx, definitions); err != nil {
		t.Fatalf("EnsureDefinitions() error = %v", err)
	}
	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := string(loaded[key].Value); got != "7" {
		t.Errorf("seeded value = %s", got)
	}
	if got := loaded[key].Tags; !reflect.DeepEqual(got, []string{"go", "test"}) {
		t.Errorf("seeded tags = %#v", got)
	}

	updated, err := store.Update(ctx, key, json.RawMessage("9"), "integration-test")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if string(updated.Value) != "9" || updated.UpdatedBy == nil || *updated.UpdatedBy != "integration-test" || updated.UpdatedAt == nil {
		t.Errorf("updated setting = %#v", updated)
	}
}
