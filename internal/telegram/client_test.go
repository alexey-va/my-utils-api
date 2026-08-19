package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendHTMLMessageUsesTelegramContract(t *testing.T) {
	t.Parallel()
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret/sendMessage" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":123}}`))
	}))
	defer server.Close()
	client := NewClient("secret", server.URL, nil)
	id, err := client.SendHTMLMessage(context.Background(), 42, "<b>hi</b>", "One:one,Two:two")
	if err != nil {
		t.Fatal(err)
	}
	if id != 123 || request["parse_mode"] != "HTML" || request["chat_id"].(float64) != 42 {
		t.Fatalf("id=%d request=%#v", id, request)
	}
	markup, ok := request["reply_markup"].(map[string]any)
	if !ok || len(markup["inline_keyboard"].([]any)) != 1 {
		t.Fatalf("markup = %#v", request["reply_markup"])
	}
}

func TestParseButtonsRows(t *testing.T) {
	t.Parallel()
	rows, err := ParseButtons("Да:yes,Нет:no;Позже:later")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(rows[0]) != 2 || rows[1][0].CallbackData != "later" {
		t.Fatalf("rows = %#v", rows)
	}
	if _, err := ParseButtons("missing separator"); err == nil {
		t.Fatal("expected invalid button error")
	}
}

func TestDerivedFileToken(t *testing.T) {
	t.Parallel()
	if got := FileUploadToken("", "bot-secret"); got != "4984054325ef99682d6a9580018f602e1fca016ff1e6070c339e9637eec037b3" {
		t.Fatalf("token = %q", got)
	}
}

func TestTelegramAPIErrorsNeverExposeBotToken(t *testing.T) {
	t.Parallel()
	const token = "123456:super-secret-token"
	t.Run("API response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request"}`))
		}))
		defer server.Close()
		assertSafeTelegramError(t, NewClient(token, server.URL, nil))
	})
	t.Run("transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		baseURL := server.URL
		server.Close()
		assertSafeTelegramError(t, NewClient(token, baseURL, nil))
	})
}

func assertSafeTelegramError(t *testing.T, client *Client) {
	t.Helper()
	_, err := client.SendHTMLMessage(context.Background(), 42, "test", "")
	if err == nil {
		t.Fatal("expected Telegram API error")
	}
	if strings.Contains(err.Error(), tokenFromClient(client)) || strings.Contains(err.Error(), "/bot") {
		t.Fatalf("Telegram error leaked bot token path: %q", err)
	}
	if !strings.Contains(err.Error(), "sendMessage") {
		t.Fatalf("Telegram error lost operation name: %q", err)
	}
}

func tokenFromClient(client *Client) string { return client.token }
