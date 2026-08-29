package telegram

import (
	"context"
	"testing"
)

type fakeVoiceResolver struct {
	called bool
	text   string
}

func (f *fakeVoiceResolver) Resolve(context.Context, Voice) (string, error) {
	f.called = true
	return f.text, nil
}

type fakeInboundMessenger struct {
	messages []string
}

func (f *fakeInboundMessenger) SendHTMLMessage(_ context.Context, _ int64, text, _ string) (int, error) {
	f.messages = append(f.messages, text)
	return 1, nil
}

func TestInboundHandlerRejectsUnauthorizedVoiceBeforeResolution(t *testing.T) {
	t.Parallel()
	resolver := &fakeVoiceResolver{text: "жим 70 три по десять"}
	messenger := &fakeInboundMessenger{}
	nextCalled := false
	rejected := 0
	handler := NewInboundHandler(map[int64]bool{7: true}, messenger, resolver, func() { rejected++ }, func(context.Context, int64, int64, string) error {
		nextCalled = true
		return nil
	})

	if err := handler.Dispatch(context.Background(), InboundMessage{ChatID: 42, UserID: 99, Voice: &Voice{FileID: "voice-1"}}); err != nil {
		t.Fatal(err)
	}
	if resolver.called || nextCalled || rejected != 1 {
		t.Fatalf("unauthorized request reached resolver=%v next=%v rejected=%d", resolver.called, nextCalled, rejected)
	}
	if len(messenger.messages) != 1 || messenger.messages[0] != "У вас нет доступа к этому боту." {
		t.Fatalf("messages = %#v", messenger.messages)
	}
}

func TestInboundHandlerPassesVoiceTranscriptToTextHandler(t *testing.T) {
	t.Parallel()
	resolver := &fakeVoiceResolver{text: "жим 70 три по десять"}
	messenger := &fakeInboundMessenger{}
	var got string
	handler := NewInboundHandler(map[int64]bool{7: true}, messenger, resolver, nil, func(_ context.Context, chatID, userID int64, text string) error {
		if chatID != 42 || userID != 7 {
			t.Fatalf("chatID=%d userID=%d", chatID, userID)
		}
		got = text
		return nil
	})

	if err := handler.Dispatch(context.Background(), InboundMessage{ChatID: 42, UserID: 7, Voice: &Voice{FileID: "voice-1"}}); err != nil {
		t.Fatal(err)
	}
	if got != "жим 70 три по десять" {
		t.Fatalf("text = %q", got)
	}
	if len(messenger.messages) != 0 {
		t.Fatalf("messages = %#v", messenger.messages)
	}
}

type fakeVoiceDownloader struct {
	fileID   string
	maxBytes int64
	data     []byte
}

func (f *fakeVoiceDownloader) DownloadFile(_ context.Context, fileID string, maxBytes int64) ([]byte, error) {
	f.fileID = fileID
	f.maxBytes = maxBytes
	return f.data, nil
}

func TestVoiceResolverProducesTranscriptFromOggDownload(t *testing.T) {
	t.Parallel()
	downloader := &fakeVoiceDownloader{data: []byte{1, 2, 3}}
	resolver := NewVoiceResolver(downloader, func(_ context.Context, model, format string, audio []byte) (string, error) {
		if model != "openai/whisper-1" || format != "ogg" || string(audio) != string([]byte{1, 2, 3}) {
			t.Fatalf("model=%q format=%q audio=%v", model, format, audio)
		}
		return "  присед 100 пять по пять  ", nil
	}, func() string { return "openai/whisper-1" }, 20<<20)

	text, err := resolver.Resolve(context.Background(), Voice{FileID: "voice-1", FileSize: 3, MimeType: "audio/ogg"})
	if err != nil {
		t.Fatal(err)
	}
	if text != "присед 100 пять по пять" {
		t.Fatalf("text = %q", text)
	}
	if downloader.fileID != "voice-1" || downloader.maxBytes != 20<<20 {
		t.Fatalf("download fileID=%q maxBytes=%d", downloader.fileID, downloader.maxBytes)
	}
}

func TestVoiceResolverRejectsEmptyDownloadBeforeSTT(t *testing.T) {
	t.Parallel()
	transcribeCalled := false
	resolver := NewVoiceResolver(&fakeVoiceDownloader{}, func(context.Context, string, string, []byte) (string, error) {
		transcribeCalled = true
		return "не должно вызываться", nil
	}, func() string { return "openai/whisper-1" }, 20<<20)

	if _, err := resolver.Resolve(context.Background(), Voice{FileID: "voice-1", MimeType: "audio/ogg"}); err == nil {
		t.Fatal("expected empty Telegram voice error")
	}
	if transcribeCalled {
		t.Fatal("empty Telegram file must not reach paid STT")
	}
}
