package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientSendsOpenAICompatibleRequestAndHeaders(t *testing.T) {
	t.Parallel()
	var got Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("HTTP-Referer") != "https://example.test" || r.Header.Get("X-Title") != "my-utils" {
			t.Fatalf("OpenRouter attribution headers missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done","tool_calls":[{"id":"call-1","type":"function","function":{"name":"list_exercises","arguments":"{}"}}]}}]}`))
	}))
	defer server.Close()

	client, err := New(Config{APIKey: "secret", BaseURL: server.URL, HTTPReferer: "https://example.test", AppTitle: "my-utils", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Complete(context.Background(), Request{Model: "provider/model", Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "provider/model" || len(got.Messages) != 1 {
		t.Fatalf("request = %#v", got)
	}
	if response.Message.Content != "done" || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Function.Name != "list_exercises" {
		t.Fatalf("response = %#v", response)
	}
}

func TestClientRetriesRetryableStatus(t *testing.T) {
	t.Parallel()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "busy", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "secret", BaseURL: server.URL, Timeout: time.Second, MaxAttempts: 2, InitialDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), Request{Model: "p/m", Messages: []Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestClientReadsRetryPolicyForEveryRequest(t *testing.T) {
	t.Parallel()
	attempts := 1
	delay := time.Millisecond
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests <= 2 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()
	client, err := New(Config{
		APIKey: "secret", BaseURL: server.URL, Timeout: time.Second,
		MaxAttemptsFunc: func() int { return attempts }, InitialDelayFunc: func() time.Duration { return delay },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), Request{Model: "p/m"}); err == nil {
		t.Fatal("first request should use the one-attempt runtime policy")
	}
	attempts = 2
	if _, err := client.Complete(context.Background(), Request{Model: "p/m"}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestClientRejectsEmptyChoice(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	client, err := New(Config{APIKey: "secret", BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), Request{Model: "p/m"}); err == nil {
		t.Fatal("expected empty-choice error")
	}
}

func TestRetryDelayMatchesBoundedLegacyBackoff(t *testing.T) {
	t.Parallel()
	if got := retryDelay(time.Second, 6); got != 30*time.Second {
		t.Fatalf("retryDelay(1s, 6) = %s", got)
	}
	if got := retryDelay(60*time.Second, 1); got != 60*time.Second {
		t.Fatalf("first configured delay = %s", got)
	}
	if got := retryDelay(60*time.Second, 2); got != 30*time.Second {
		t.Fatalf("second bounded delay = %s", got)
	}
}
