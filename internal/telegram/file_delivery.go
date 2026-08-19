package telegram

import (
	"context"
	"net/http"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/workout"
)

const MaxFileSize = 20 * 1024 * 1024

type DocumentSender interface {
	SendDocument(context.Context, int64, []byte, string, string, string) error
}

type FileUploadResponse struct {
	OK        bool   `json:"ok"`
	FileName  string `json:"fileName"`
	SizeBytes int64  `json:"sizeBytes"`
	SentTo    int    `json:"sentTo"`
}

type FileDelivery struct {
	expectedToken string
	chatIDs       []int64
	sender        DocumentSender
}

func NewFileDelivery(expectedToken string, chatIDs []int64, sender DocumentSender) *FileDelivery {
	return &FileDelivery{expectedToken: strings.TrimSpace(expectedToken), chatIDs: append([]int64(nil), chatIDs...), sender: sender}
}

func (s *FileDelivery) Deliver(ctx context.Context, token, fileName, contentType, caption string, data []byte) (FileUploadResponse, error) {
	if s.expectedToken == "" {
		return FileUploadResponse{}, fileError(http.StatusServiceUnavailable, "Telegram file upload authentication is not configured")
	}
	if !TokenMatches(s.expectedToken, token) {
		return FileUploadResponse{}, fileError(http.StatusUnauthorized, "Invalid Telegram file token")
	}
	if len(data) == 0 {
		return FileUploadResponse{}, fileError(http.StatusBadRequest, "File is empty")
	}
	if len(data) > MaxFileSize {
		return FileUploadResponse{}, fileError(http.StatusRequestEntityTooLarge, "File is larger than 20 MB")
	}
	if len(s.chatIDs) == 0 {
		return FileUploadResponse{}, fileError(http.StatusServiceUnavailable, "TELEGRAM_ALLOWED_USER_IDS is not configured")
	}
	if s.sender == nil {
		return FileUploadResponse{}, fileError(http.StatusServiceUnavailable, "Telegram bot is not configured")
	}
	fileName = SafeFileName(fileName)
	sent := 0
	for _, chatID := range s.chatIDs {
		if err := s.sender.SendDocument(ctx, chatID, data, fileName, contentType, strings.TrimSpace(caption)); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return FileUploadResponse{}, fileError(http.StatusBadGateway, "Telegram did not accept the file")
	}
	return FileUploadResponse{OK: true, FileName: fileName, SizeBytes: int64(len(data)), SentTo: sent}, nil
}

func fileError(status int, message string) error {
	return &workout.Error{Status: status, Message: message}
}
