package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

const (
	DefaultMaxVoiceBytes int64 = 20 << 20
	WorkoutStartMessage        = "Тренер по дневнику. Можно писать текстом или присылать голосовое. Напиши «что на сегодня» — скажу, что уже было, и предложу план. Или сразу запиши подход: «жим 70 3*10/12»."
)

type VoiceTextResolver interface {
	Resolve(context.Context, Voice) (string, error)
}

type inboundMessenger interface {
	SendHTMLMessage(context.Context, int64, string, string) (int, error)
}

type TextHandler func(context.Context, int64, int64, string) error

type InboundHandler struct {
	allowed    map[int64]bool
	messenger  inboundMessenger
	voices     VoiceTextResolver
	onRejected func()
	next       TextHandler
}

func NewInboundHandler(allowed map[int64]bool, messenger inboundMessenger, voices VoiceTextResolver, onRejected func(), next TextHandler) *InboundHandler {
	return &InboundHandler{allowed: allowed, messenger: messenger, voices: voices, onRejected: onRejected, next: next}
}

func (h *InboundHandler) Dispatch(ctx context.Context, message InboundMessage) error {
	if len(h.allowed) > 0 && !h.allowed[message.UserID] {
		if h.onRejected != nil {
			h.onRejected()
		}
		_, err := h.messenger.SendHTMLMessage(ctx, message.ChatID, "У вас нет доступа к этому боту.", "")
		return err
	}
	text := strings.TrimSpace(message.Text)
	if message.Voice != nil {
		var err error
		text, err = h.voices.Resolve(ctx, *message.Voice)
		if err != nil {
			slog.WarnContext(ctx, "Telegram voice transcription failed", "chatId", message.ChatID, "error", err)
			_, sendErr := h.messenger.SendHTMLMessage(ctx, message.ChatID, "❌ Не удалось распознать голосовое сообщение. Попробуй ещё раз или отправь текстом.", "")
			return sendErr
		}
	}
	if text == "" {
		return errors.New("Telegram inbound message has no text")
	}
	return h.next(ctx, message.ChatID, message.UserID, text)
}

type VoiceFileDownloader interface {
	DownloadFile(context.Context, string, int64) ([]byte, error)
}

type AudioTranscribeFunc func(context.Context, string, string, []byte) (string, error)

type VoiceResolver struct {
	files      VoiceFileDownloader
	transcribe AudioTranscribeFunc
	model      func() string
	maxBytes   int64
}

func NewVoiceResolver(files VoiceFileDownloader, transcribe AudioTranscribeFunc, model func() string, maxBytes int64) *VoiceResolver {
	return &VoiceResolver{files: files, transcribe: transcribe, model: model, maxBytes: maxBytes}
}

func (r *VoiceResolver) Resolve(ctx context.Context, voice Voice) (string, error) {
	if strings.TrimSpace(voice.FileID) == "" {
		return "", errors.New("Telegram voice file_id is empty")
	}
	if voice.FileSize > r.maxBytes {
		return "", fmt.Errorf("Telegram voice is too large: %d bytes exceeds %d", voice.FileSize, r.maxBytes)
	}
	format, err := voiceInputFormat(voice.MimeType)
	if err != nil {
		return "", err
	}
	audio, err := r.files.DownloadFile(ctx, voice.FileID, r.maxBytes)
	if err != nil {
		return "", fmt.Errorf("download Telegram voice: %w", err)
	}
	if len(audio) == 0 {
		return "", errors.New("Telegram returned an empty voice file")
	}
	text, err := r.transcribe(ctx, strings.TrimSpace(r.model()), format, audio)
	if err != nil {
		return "", fmt.Errorf("transcribe Telegram voice: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("OpenRouter returned an empty voice transcript")
	}
	return text, nil
}

func voiceInputFormat(mimeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])) {
	case "", "audio/ogg", "application/ogg":
		return "ogg", nil
	case "audio/mpeg", "audio/mp3":
		return "mp3", nil
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return "m4a", nil
	case "audio/webm":
		return "webm", nil
	case "audio/aac":
		return "aac", nil
	default:
		return "", fmt.Errorf("unsupported Telegram voice MIME type %q", mimeType)
	}
}
