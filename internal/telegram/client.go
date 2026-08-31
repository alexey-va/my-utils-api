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
	"path"
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

type APIError struct {
	Method      string
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Telegram API %s failed: %d %s", e.Method, e.Code, e.Description)
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

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
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
	return c.editHTMLMessage(ctx, chatID, messageID, text, nil)
}

func (c *Client) EditHTMLMessageWithButtons(ctx context.Context, chatID int64, messageID int, text, buttons string) error {
	rows, err := ParseButtons(buttons)
	if err != nil {
		return err
	}
	markup := inlineMarkup{InlineKeyboard: rows}
	return c.editHTMLMessage(ctx, chatID, messageID, text, &markup)
}

func (c *Client) editHTMLMessage(ctx context.Context, chatID int64, messageID int, text string, markup *inlineMarkup) error {
	payload := map[string]any{"chat_id": chatID, "message_id": messageID, "text": truncateRunes(text, messageMaxLength), "parse_mode": "HTML"}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	err := c.callJSON(ctx, "editMessageText", payload, nil)
	var apiError *APIError
	if errors.As(err, &apiError) && apiError.Code == http.StatusBadRequest && strings.Contains(strings.ToLower(apiError.Description), "message is not modified") {
		return nil
	}
	return err
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

func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	return c.callJSON(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
}

func (c *Client) SetMyCommandsForChat(ctx context.Context, chatID int64, commands []BotCommand) error {
	scope := map[string]any{"type": "chat", "chat_id": chatID}
	return c.callJSON(ctx, "setMyCommands", map[string]any{"commands": commands, "scope": scope}, nil)
}

func (c *Client) DeleteWebhook(ctx context.Context, dropPending bool) error {
	return c.callJSON(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": dropPending}, nil)
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, png []byte, caption string) error {
	return c.sendPhoto(ctx, chatID, png, caption, false)
}

func (c *Client) SendProtectedPhoto(ctx context.Context, chatID int64, png []byte, caption string) error {
	return c.sendPhoto(ctx, chatID, png, caption, true)
}

func (c *Client) sendPhoto(ctx context.Context, chatID int64, png []byte, caption string, protected bool) error {
	fields := map[string]string{"chat_id": strconv.FormatInt(chatID, 10)}
	if protected {
		fields["protect_content"] = "true"
	}
	if caption = strings.TrimSpace(caption); caption != "" {
		fields["caption"] = truncateRunes(caption, captionMaxLength)
		fields["parse_mode"] = "HTML"
	}
	return c.callMultipart(ctx, "sendPhoto", fields, "photo", "chart.png", "image/png", png)
}

func (c *Client) SendDocument(ctx context.Context, chatID int64, data []byte, fileName, contentType, caption string) error {
	return c.sendDocument(ctx, chatID, data, fileName, contentType, caption, false)
}

func (c *Client) SendProtectedDocument(ctx context.Context, chatID int64, data []byte, fileName, contentType, caption string) error {
	return c.sendDocument(ctx, chatID, data, fileName, contentType, caption, true)
}

func (c *Client) sendDocument(ctx context.Context, chatID int64, data []byte, fileName, contentType, caption string, protected bool) error {
	fileName = SafeFileName(fileName)
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	fields := map[string]string{"chat_id": strconv.FormatInt(chatID, 10)}
	if protected {
		fields["protect_content"] = "true"
	}
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
	Voice     *Voice `json:"voice"`
}
type Voice struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type"`
}
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type File struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	FilePath string `json:"file_path"`
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	var result []Update
	err := c.callJSON(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": timeoutSeconds, "allowed_updates": []string{"message", "edited_message", "callback_query"}}, &result)
	return result, err
}

func (c *Client) DownloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, errors.New("Telegram file_id is empty")
	}
	if maxBytes < 1 {
		return nil, errors.New("Telegram file size limit must be positive")
	}
	var file File
	if err := c.callJSON(ctx, "getFile", map[string]any{"file_id": fileID}, &file); err != nil {
		return nil, err
	}
	if file.FileSize > maxBytes {
		return nil, fmt.Errorf("Telegram file is too large: %d bytes exceeds %d", file.FileSize, maxBytes)
	}
	escapedPath, err := escapeTelegramFilePath(file.FilePath)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/file/bot"+c.token+"/"+escapedPath, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			return nil, fmt.Errorf("Telegram file download failed: %w", urlError.Err)
		}
		return nil, errors.New("Telegram file download failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Telegram file download failed: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Telegram file: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("Telegram file is too large: downloaded more than %d bytes", maxBytes)
	}
	return data, nil
}

func escapeTelegramFilePath(raw string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(raw))
	if cleaned == "/" {
		return "", errors.New("Telegram getFile succeeded without file_path")
	}
	segments := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return strings.Join(segments, "/"), nil
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
		return &APIError{Method: method, Code: envelope.ErrorCode, Description: envelope.Description}
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
