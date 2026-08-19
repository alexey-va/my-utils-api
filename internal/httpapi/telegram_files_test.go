package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/telegram"
)

type fakeTelegramFiles struct {
	token, name, caption string
	data                 []byte
}

func (f *fakeTelegramFiles) Deliver(_ context.Context, token, name, _ string, caption string, data []byte) (telegram.FileUploadResponse, error) {
	f.token, f.name, f.caption, f.data = token, name, caption, append([]byte(nil), data...)
	return telegram.FileUploadResponse{OK: true, FileName: telegram.SafeFileName(name), SizeBytes: int64(len(data)), SentTo: 1}, nil
}

func TestTelegramFileUploadRoute(t *testing.T) {
	t.Parallel()
	service := &fakeTelegramFiles{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "../report.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = writer.WriteField("caption", "ready")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/telegram/files", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Telegram-File-Token", "token")
	response := httptest.NewRecorder()
	NewRouter(Dependencies{TelegramFiles: service}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.token != "token" || service.name != "report.txt" || string(service.data) != "hello" {
		t.Fatalf("status=%d service=%#v body=%s", response.Code, service, response.Body.String())
	}
}
