package wireguardops

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"text/template"
)

func TestMetalDiscordTruncatedAlertsCounter(t *testing.T) {
	raw, err := os.ReadFile("../../observability/config/grafana-metal-discord-template.txt")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, `{{- $content := "" -}}`)
	if start < 0 {
		t.Fatal("notification template has no counter block")
	}
	end := strings.Index(source[start:], "{{- coll.Dict")
	if end < 0 {
		t.Fatal("notification template has no payload after its counter block")
	}
	// Grafana's namespaced JSON helper is exposed as a plain FuncMap entry here.
	block := strings.ReplaceAll(source[start:start+end], "data.ToJSON", "toJSON")
	parsed, err := template.New("counter").Funcs(template.FuncMap{
		"toJSON": func(value any) (string, error) {
			encoded, err := json.Marshal(value)
			return string(encoded), err
		},
	}).Parse(block + "{{ $content }}")
	if err != nil {
		t.Fatal(err)
	}
	zero, one, seven := 0, 1, 7
	for _, tc := range []struct {
		name  string
		count *int
		want  string
	}{
		{name: "absent"},
		{name: "zero", count: &zero},
		{name: "one", count: &one, want: "Показаны первые 5 тревог; ещё 1 скрыты лимитом webhook."},
		{name: "seven", count: &seven, want: "Показаны первые 5 тревог; ещё 7 скрыты лимитом webhook."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Webhook Notify always supplies *int, including a non-nil pointer to zero.
			data := struct {
				TruncatedAlerts *int
				Alerts          []struct{}
			}{TruncatedAlerts: tc.count, Alerts: make([]struct{}, 5)}
			var rendered bytes.Buffer
			if err := parsed.Execute(&rendered, data); err != nil {
				t.Fatal(err)
			}
			if got := rendered.String(); got != tc.want {
				t.Fatalf("counter text = %q, want %q", got, tc.want)
			}
		})
	}
}
