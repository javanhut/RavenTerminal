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

type Client struct {
	BaseURL   string
	Model     string
	KeepAlive string
	HTTP      *http.Client
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		BaseURL:   normalizeBaseURL(baseURL),
		Model:     strings.TrimSpace(model),
		KeepAlive: "5m",
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
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
	var resp generateResponse
	if err := c.postJSON(ctx, "/api/generate", req, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
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
	if c.BaseURL == "" {
		return "", errors.New("ollama url not set")
	}
	if c.Model == "" {
		return "", errors.New("ollama model not set")
	}

	req := chatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
	}

	endpoint := c.BaseURL + "/api/chat"
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Use a client without timeout for streaming - context handles cancellation
	streamClient := &http.Client{}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			var errResp struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error != "" {
				return "", fmt.Errorf("ollama: %s", errResp.Error)
			}
			errMsg := strings.TrimSpace(string(bodyBytes))
			if len(errMsg) > 200 {
				errMsg = errMsg[:200]
			}
			return "", fmt.Errorf("ollama: %s", errMsg)
		}
		return "", fmt.Errorf("ollama api error (%s)", resp.Status)
	}

	var fullContent strings.Builder
	decoder := json.NewDecoder(resp.Body)
	for {
		var streamResp chatStreamResponse
		if err := decoder.Decode(&streamResp); err != nil {
			if err == io.EOF {
				break
			}
			// Check if context was cancelled
			if ctx.Err() != nil {
				return fullContent.String(), ctx.Err()
			}
			return fullContent.String(), err
		}
		if streamResp.Error != "" {
			return fullContent.String(), errors.New(streamResp.Error)
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

	result := fullContent.String()
	if strings.TrimSpace(result) == "" {
		return "", errors.New("empty response")
	}
	return result, nil
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

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Error   string  `json:"error"`
}

type chatStreamResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error"`
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
