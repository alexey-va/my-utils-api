package telegram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	messageMaxLength = 4096
	captionMaxLength = 1024
)

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

func NewClient(token, baseURL string, proxy *url.URL) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &Client{token: strings.TrimSpace(token), baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), http: &http.Client{Transport: transport, Timeout: 3 * time.Minute}}
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type inlineMarkup struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

func ParseButtons(raw string) ([][]InlineButton, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	result := [][]InlineButton{}
	for _, rowRaw := range strings.Split(raw, ";") {
		row := []InlineButton{}
		for _, buttonRaw := range strings.Split(rowRaw, ",") {
			parts := strings.SplitN(strings.TrimSpace(buttonRaw), ":", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return nil, fmt.Errorf("неверный формат кнопки %q", buttonRaw)
			}
			callback := strings.TrimSpace(parts[1])
			if len(callback) > 64 {
				return nil, errors.New("callback_data длиннее 64 байт")
			}
			row = append(row, InlineButton{Text: strings.TrimSpace(parts[0]), CallbackData: callback})
		}
		if len(row) > 0 {
			result = append(result, row)
		}
	}
	return result, nil
}

func (c *Client) SendHTMLMessage(ctx context.Context, chatID int64, text, buttons string) (int, error) {
	payload := map[string]any{"chat_id": chatID, "text": truncateRunes(text, messageMaxLength), "parse_mode": "HTML"}
	if rows, err := ParseButtons(buttons); err != nil {
		return 0, err
	} else if len(rows) > 0 {
		payload["reply_markup"] = inlineMarkup{InlineKeyboard: rows}
	}
	var response struct {
		MessageID int `json:"message_id"`
	}
	if err := c.callJSON(ctx, "sendMessage", payload, &response); err != nil {
		return 0, err
	}
	if response.MessageID == 0 {
		return 0, errors.New("Telegram sendMessage succeeded without message_id")
	}
	return response.MessageID, nil
}

func (c *Client) EditHTMLMessage(ctx context.Context, chatID int64, messageID int, text string) error {
	return c.callJSON(ctx, "editMessageText", map[string]any{"chat_id": chatID, "message_id": messageID, "text": truncateRunes(text, messageMaxLength), "parse_mode": "HTML"}, nil)
}

func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	return c.callJSON(ctx, "deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
}

func (c *Client) SendTyping(ctx context.Context, chatID int64) error {
	return c.callJSON(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": "typing"}, nil)
}

func (c *Client) AnswerCallback(ctx context.Context, callbackID string) error {
	return c.callJSON(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callbackID}, nil)
}

func (c *Client) DeleteWebhook(ctx context.Context, dropPending bool) error {
	return c.callJSON(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": dropPending}, nil)
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, png []byte, caption string) error {
	fields := map[string]string{"chat_id": strconv.FormatInt(chatID, 10)}
	if caption = strings.TrimSpace(caption); caption != "" {
		fields["caption"] = truncateRunes(caption, captionMaxLength)
		fields["parse_mode"] = "HTML"
	}
	return c.callMultipart(ctx, "sendPhoto", fields, "photo", "chart.png", "image/png", png)
}

func (c *Client) SendDocument(ctx context.Context, chatID int64, data []byte, fileName, contentType, caption string) error {
	fileName = SafeFileName(fileName)
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	fields := map[string]string{"chat_id": strconv.FormatInt(chatID, 10)}
	if caption = strings.TrimSpace(caption); caption != "" {
		fields["caption"] = truncateRunes(caption, captionMaxLength)
	}
	return c.callMultipart(ctx, "sendDocument", fields, "document", fileName, contentType, data)
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	EditedMessage *Message       `json:"edited_message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}
type User struct {
	ID int64 `json:"id"`
}
type Chat struct {
	ID int64 `json:"id"`
}
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	var result []Update
	err := c.callJSON(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": timeoutSeconds, "allowed_updates": []string{"message", "edited_message", "callback_query"}}, &result)
	return result, err
}

func (c *Client) callJSON(ctx context.Context, method string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(method), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	return c.do(request, method, result)
}

func (c *Client) callMultipart(ctx context.Context, method string, fields map[string]string, fieldName, fileName, contentType string, data []byte) error {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	header := make(textproto.MIMEHeader)
	header["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, fieldName, fileName)}
	header["Content-Type"] = []string{contentType}
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(method), &buffer)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return c.do(request, method, nil)
}

func (c *Client) do(request *http.Request, method string, result any) error {
	response, err := c.http.Do(request)
	if err != nil {
		// net/http wraps transport failures in url.Error whose Error string
		// contains the full request URL. Telegram embeds the bot token in that
		// URL, so keep the underlying cause but never wrap the outer URL error.
		var urlError *url.Error
		if errors.As(err, &urlError) {
			return fmt.Errorf("Telegram API %s request failed: %w", method, urlError.Err)
		}
		return fmt.Errorf("Telegram API %s request failed", method)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode Telegram response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.OK {
		return fmt.Errorf("Telegram API %s failed: %d %s", method, envelope.ErrorCode, envelope.Description)
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) methodURL(method string) string { return c.baseURL + "/bot" + c.token + "/" + method }

func FileUploadToken(explicit, botToken string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	botToken = strings.TrimSpace(botToken)
	if botToken == "" {
		return ""
	}
	digest := sha256.Sum256([]byte("my-utils-file-upload:" + botToken))
	return hex.EncodeToString(digest[:])
}

func TokenMatches(expected, provided string) bool {
	if expected == "" || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func SafeFileName(original string) string {
	normalized := filepath.Base(strings.ReplaceAll(strings.TrimSpace(original), `\`, "/"))
	if normalized == "." || normalized == "/" || normalized == "" {
		return "file.bin"
	}
	runes := []rune(normalized)
	if len(runes) > 255 {
		runes = runes[:255]
	}
	return string(runes)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}
