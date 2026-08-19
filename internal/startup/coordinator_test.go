package startup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunDoesNotServeUntilEveryWarmupCompletes(t *testing.T) {
	t.Parallel()

	var events []string
	warmers := []Warmer{
		fakeWarmer{name: "database", warm: func(context.Context) error {
			events = append(events, "database")
			return nil
		}},
		fakeWarmer{name: "runtime-settings", warm: func(context.Context) error {
			events = append(events, "runtime-settings")
			return nil
		}},
		fakeWarmer{name: "jwt", warm: func(context.Context) error {
			events = append(events, "jwt")
			return nil
		}},
	}

	err := Run(context.Background(), warmers, func(context.Context) error {
		events = append(events, "serve")
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []string{"database", "runtime-settings", "jwt", "serve"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestRunFailsClosedWhenWarmupFails(t *testing.T) {
	t.Parallel()

	var events []string
	warmers := []Warmer{
		fakeWarmer{name: "database", warm: func(context.Context) error {
			events = append(events, "database")
			return errors.New("connection refused")
		}},
		fakeWarmer{name: "jwt", warm: func(context.Context) error {
			events = append(events, "jwt")
			return nil
		}},
	}

	err := Run(context.Background(), warmers, func(context.Context) error {
		events = append(events, "serve")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "database") || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Run() error = %v", err)
	}
	if want := []string{"database"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

type fakeWarmer struct {
	name string
	warm func(context.Context) error
}

func (w fakeWarmer) Name() string { return w.name }

func (w fakeWarmer) Warm(ctx context.Context) error { return w.warm(ctx) }
