package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ThinkingOptions configures thinking/reasoning mode for supported models
type ThinkingOptions struct {
	Enabled bool // Enable thinking mode
	Budget  int  // Max tokens for thinking (0 = no limit)
}

// ChatResult contains the response and any thinking content
type ChatResult struct {
	Content  string // The main response content
	Thinking string // Thinking/reasoning content (if any)
}

type Client struct {
	BaseURL   string
	Model     string
	KeepAlive string
	HTTP      *http.Client
	Thinking  ThinkingOptions
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		BaseURL:   normalizeBaseURL(baseURL),
		Model:     strings.TrimSpace(model),
		KeepAlive: "5m",
		HTTP: &http.Client{
			Timeout: 180 * time.Second, // Increased for slow remote APIs
			Transport: &http.Transport{
				TLSHandshakeTimeout:   30 * time.Second,
				ResponseHeaderTimeout: 120 * time.Second,
				ExpectContinueTimeout: 5 * time.Second,
			},
		},
	}
}

func (c *Client) LoadModel(ctx context.Context) error {
	if c.BaseURL == "" {
		return errors.New("ollama url not set")
	}
	if c.Model == "" {
		return errors.New("ollama model not set")
	}

	req := generateRequest{
		Model:     c.Model,
		Prompt:    " ",
		Stream:    false,
		KeepAlive: c.KeepAlive,
	}

	// Retry with exponential backoff for flaky connections
	maxRetries := 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2s, 4s
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled during retry: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		var resp generateResponse
		if err := c.postJSON(ctx, "/api/generate", req, &resp); err != nil {
			lastErr = c.wrapError(err)
			// Only retry on timeout/network errors
			if !isRetryableError(err) {
				return lastErr
			}
			continue
		}
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return nil
	}
	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// wrapError provides more helpful error messages for common failures
func (c *Client) wrapError(err error) error {
	if err == nil {
		return nil
	}
	errStr := err.Error()
	if strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "Client.Timeout") {
		return fmt.Errorf("connection timeout - server at %s is not responding (try checking if the server is running)", c.BaseURL)
	}
	if strings.Contains(errStr, "connection refused") {
		return fmt.Errorf("connection refused - no server running at %s", c.BaseURL)
	}
	if strings.Contains(errStr, "no such host") {
		return fmt.Errorf("unknown host - could not resolve %s", c.BaseURL)
	}
	if strings.Contains(errStr, "certificate") {
		return fmt.Errorf("TLS/SSL error - certificate issue with %s", c.BaseURL)
	}
	return err
}

// isRetryableError returns true if the error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "Client.Timeout") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "temporary failure")
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c.BaseURL == "" {
		return "", errors.New("ollama url not set")
	}
	if c.Model == "" {
		return "", errors.New("ollama model not set")
	}

	req := chatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   false,
	}
	var resp chatResponse
	if err := c.postJSON(ctx, "/api/chat", req, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}
	if strings.TrimSpace(resp.Message.Content) == "" {
		return "", errors.New("empty response")
	}
	return resp.Message.Content, nil
}

// ChatStream sends a streaming chat request and calls onToken for each received token.
// Returns the full accumulated response when done.
func (c *Client) ChatStream(ctx context.Context, messages []Message, onToken func(token string)) (string, error) {
	result, err := c.ChatStreamWithThinking(ctx, messages, onToken, nil)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// ChatStreamWithThinking sends a streaming chat request with thinking mode support.
// onToken is called for each content token, onThinking is called for thinking tokens.
// Returns ChatResult with both content and thinking.
func (c *Client) ChatStreamWithThinking(ctx context.Context, messages []Message, onToken func(token string), onThinking func(token string)) (ChatResult, error) {
	if c.BaseURL == "" {
		return ChatResult{}, errors.New("ollama url not set")
	}
	if c.Model == "" {
		return ChatResult{}, errors.New("ollama model not set")
	}

	req := chatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
	}

	// Add thinking options if enabled
	if c.Thinking.Enabled {
		req.Think = true
		if c.Thinking.Budget > 0 {
			req.Options = &chatOptions{
				ThinkingBudget: c.Thinking.Budget,
			}
		}
	}

	endpoint := c.BaseURL + "/api/chat"
	body, err := json.Marshal(req)
	if err != nil {
		return ChatResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Use a client without timeout for streaming - context handles cancellation
	streamClient := &http.Client{}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			var errResp struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error != "" {
				return ChatResult{}, fmt.Errorf("ollama: %s", errResp.Error)
			}
			errMsg := strings.TrimSpace(string(bodyBytes))
			if len(errMsg) > 200 {
				errMsg = errMsg[:200]
			}
			return ChatResult{}, fmt.Errorf("ollama: %s", errMsg)
		}
		return ChatResult{}, fmt.Errorf("ollama api error (%s)", resp.Status)
	}

	var fullContent strings.Builder
	var fullThinking strings.Builder
	decoder := json.NewDecoder(resp.Body)

	for {
		var streamResp chatStreamResponse
		if err := decoder.Decode(&streamResp); err != nil {
			if err == io.EOF {
				break
			}
			// Check if context was cancelled
			if ctx.Err() != nil {
				return ChatResult{Content: fullContent.String(), Thinking: fullThinking.String()}, ctx.Err()
			}
			return ChatResult{Content: fullContent.String(), Thinking: fullThinking.String()}, err
		}
		if streamResp.Error != "" {
			return ChatResult{Content: fullContent.String(), Thinking: fullThinking.String()}, errors.New(streamResp.Error)
		}

		// Handle thinking field if present (some APIs send it separately)
		if streamResp.Thinking != "" {
			fullThinking.WriteString(streamResp.Thinking)
			if onThinking != nil {
				onThinking(streamResp.Thinking)
			}
		}

		if streamResp.Message.Content != "" {
			fullContent.WriteString(streamResp.Message.Content)
			if onToken != nil {
				onToken(streamResp.Message.Content)
			}
		}
		if streamResp.Done {
			break
		}
	}

	content := fullContent.String()
	thinking := fullThinking.String()

	// If no separate thinking field, try to extract from <think> tags in content
	if thinking == "" && strings.Contains(content, "<think>") {
		content, thinking = ExtractThinking(content)
	}

	if strings.TrimSpace(content) == "" && strings.TrimSpace(thinking) == "" {
		return ChatResult{}, errors.New("empty response")
	}

	return ChatResult{Content: content, Thinking: thinking}, nil
}

// ExtractThinking extracts thinking content from <think>...</think> tags.
// Returns the content with thinking removed, and the extracted thinking.
func ExtractThinking(content string) (string, string) {
	var thinking strings.Builder
	result := content

	for {
		start := strings.Index(result, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "</think>")
		if end == -1 {
			// Unclosed tag - treat rest as thinking
			thinking.WriteString(strings.TrimSpace(result[start+7:]))
			result = result[:start]
			break
		}
		end += start

		// Extract thinking content
		thinkContent := strings.TrimSpace(result[start+7 : end])
		if thinking.Len() > 0 {
			thinking.WriteString("\n\n")
		}
		thinking.WriteString(thinkContent)

		// Remove the thinking block from result
		result = result[:start] + result[end+8:]
	}

	return strings.TrimSpace(result), strings.TrimSpace(thinking.String())
}

func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if c.BaseURL == "" {
		return nil, errors.New("ollama url not set")
	}

	var resp tagsResponse
	if err := c.getJSON(ctx, "/api/tags", &resp); err != nil {
		return nil, err
	}
	if len(resp.Models) == 0 {
		return []string{}, nil
	}
	models := make([]string, 0, len(resp.Models))
	for _, model := range resp.Models {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = strings.TrimSpace(model.Model)
		}
		if name != "" {
			models = append(models, name)
		}
	}
	return models, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	endpoint := c.BaseURL + path
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to read error message from response body
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			var errResp struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error != "" {
				return fmt.Errorf("ollama: %s", errResp.Error)
			}
			errMsg := strings.TrimSpace(string(bodyBytes))
			if len(errMsg) > 200 {
				errMsg = errMsg[:200]
			}
			return fmt.Errorf("ollama: %s", errMsg)
		}
		return fmt.Errorf("ollama api error (%s)", resp.Status)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	endpoint := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to read error message from response body
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			var errResp struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error != "" {
				return fmt.Errorf("ollama: %s", errResp.Error)
			}
			errMsg := strings.TrimSpace(string(bodyBytes))
			if len(errMsg) > 200 {
				errMsg = errMsg[:200]
			}
			return fmt.Errorf("ollama: %s", errMsg)
		}
		return fmt.Errorf("ollama api error (%s)", resp.Status)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

type generateRequest struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Stream    bool   `json:"stream"`
	KeepAlive string `json:"keep_alive,omitempty"`
}

type generateResponse struct {
	Error string `json:"error"`
}

type chatOptions struct {
	Temperature    float32 `json:"temperature,omitempty"`
	NumPredict     int     `json:"num_predict,omitempty"`     // Max tokens to generate
	ThinkingBudget int     `json:"thinking_budget,omitempty"` // Thinking tokens limit (DeepSeek-style)
}

type chatRequest struct {
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	Stream   bool         `json:"stream"`
	Think    bool         `json:"think,omitempty"`   // Enable thinking mode (some APIs)
	Options  *chatOptions `json:"options,omitempty"` // Model options
}

type chatResponse struct {
	Message Message `json:"message"`
	Error   string  `json:"error"`
}

type chatStreamResponse struct {
	Message  Message `json:"message"`
	Thinking string  `json:"thinking,omitempty"` // Separate thinking field (some APIs)
	Done     bool    `json:"done"`
	Error    string  `json:"error"`
}

type tagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/")
}
