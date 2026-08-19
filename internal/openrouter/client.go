package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Arguments   string         `json:"arguments,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type Response struct {
	Message Message
}

type apiResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type Config struct {
	APIKey       string
	BaseURL      string
	HTTPReferer  string
	AppTitle     string
	Timeout      time.Duration
	MaxAttempts  int
	InitialDelay time.Duration
	// Runtime callbacks let admin settings take effect without rebuilding the client.
	MaxAttemptsFunc  func() int
	InitialDelayFunc func() time.Duration
	ProxyURL         string
}

type Client struct {
	config Config
	http   *http.Client
}

func New(config Config) (*Client, error) {
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if config.APIKey == "" {
		return nil, errors.New("OpenRouter API key is empty")
	}
	if config.BaseURL == "" {
		return nil, errors.New("OpenRouter base URL is empty")
	}
	if config.Timeout <= 0 {
		config.Timeout = 3 * time.Minute
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}
	if config.InitialDelay <= 0 {
		config.InitialDelay = time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(config.ProxyURL) != "" {
		proxy, err := url.Parse(config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse OpenRouter proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &Client{config: config, http: &http.Client{Transport: transport, Timeout: config.Timeout}}, nil
}

func (c *Client) Complete(ctx context.Context, request Request) (Response, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return Response{}, fmt.Errorf("encode OpenRouter request: %w", err)
	}
	var last error
	maxAttempts := c.maxAttempts()
	initialDelay := c.initialDelay()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, retryable, requestErr := c.completeOnce(ctx, payload)
		if requestErr == nil {
			return response, nil
		}
		last = requestErr
		if !retryable || attempt == maxAttempts {
			break
		}
		delay := retryDelay(initialDelay, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	return Response{}, last
}

func retryDelay(initial time.Duration, failedAttempt int) time.Duration {
	delay := initial
	for step := 1; step < failedAttempt; step++ {
		if delay > 15*time.Second {
			return 30 * time.Second
		}
		delay *= 2
	}
	return delay
}

func (c *Client) maxAttempts() int {
	value := c.config.MaxAttempts
	if c.config.MaxAttemptsFunc != nil {
		value = c.config.MaxAttemptsFunc()
	}
	if value < 1 {
		return 1
	}
	return min(value, 10)
}

func (c *Client) initialDelay() time.Duration {
	value := c.config.InitialDelay
	if c.config.InitialDelayFunc != nil {
		value = c.config.InitialDelayFunc()
	}
	if value <= 0 {
		return time.Second
	}
	return min(value, 60*time.Second)
}

func (c *Client) completeOnce(ctx context.Context, payload []byte) (Response, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if c.config.HTTPReferer != "" {
		req.Header.Set("HTTP-Referer", c.config.HTTPReferer)
	}
	if c.config.AppTitle != "" {
		req.Header.Set("X-Title", c.config.AppTitle)
	}
	response, err := c.http.Do(req)
	if err != nil {
		var netErr net.Error
		return Response{}, errors.As(err, &netErr), fmt.Errorf("OpenRouter request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return Response{}, true, fmt.Errorf("read OpenRouter response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
			fmt.Errorf("OpenRouter returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded apiResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Response{}, false, fmt.Errorf("decode OpenRouter response: %w", err)
	}
	if decoded.Error != nil {
		return Response{}, false, errors.New(decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return Response{}, false, errors.New("OpenRouter returned no choices")
	}
	return Response{Message: decoded.Choices[0].Message}, false, nil
}
