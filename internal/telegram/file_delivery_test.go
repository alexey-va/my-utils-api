package telegram

import (
	"context"
	"testing"
)

type fakeDocumentSender struct {
	recipients []int64
	fileName   string
	data       []byte
}

func (f *fakeDocumentSender) SendDocument(_ context.Context, chatID int64, data []byte, fileName, _, _ string) error {
	f.recipients = append(f.recipients, chatID)
	f.fileName, f.data = fileName, append([]byte(nil), data...)
	return nil
}

func TestFileDeliveryAuthenticatesSanitizesAndFansOut(t *testing.T) {
	t.Parallel()
	sender := &fakeDocumentSender{}
	service := NewFileDelivery("test-token", []int64{42, 43}, sender)
	result, err := service.Deliver(context.Background(), "test-token", "../report.txt", "text/plain", "caption", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if result.FileName != "report.txt" || result.SizeBytes != 5 || result.SentTo != 2 || sender.fileName != "report.txt" {
		t.Fatalf("result=%#v sender=%#v", result, sender)
	}
	if _, err := service.Deliver(context.Background(), "wrong", "a.txt", "text/plain", "", []byte("x")); err == nil {
		t.Fatal("expected authentication error")
	}
}
