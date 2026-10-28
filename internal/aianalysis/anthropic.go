package aianalysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Messages API constants, as the monitor's assistant (frontend/lib/assistant/model.js).
const (
	AnthropicURL     = "https://api.anthropic.com/v1/messages"
	AnthropicVersion = "2026-10-21"
	maxTokens        = 4000
	callTimeout      = 3 * time.Minute
)

// Anthropic is the AnalysisModel over the Messages API: one request per
// analysis, never retried. The key is never logged.
type Anthropic struct {
	APIKey string
	Model  string
	URL    string       // "" = AnthropicURL (tests point it at a fake server)
	Client *http.Client // nil = a client with callTimeout
}

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesReply struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Analyze sends the prompt and the report and returns the reply's text.
func (a *Anthropic) Analyze(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(messagesRequest{Model: a.Model, MaxTokens: maxTokens, System: system,
		Messages: []message{{Role: "user", Content: user}}})
	if err != nil {
		return "", err
	}
	url := a.URL
	if url == "" {
		url = AnthropicURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", AnthropicVersion)
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: callTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("messages API: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("messages API: read reply: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("messages API answered %d", res.StatusCode)
	}
	var r messagesReply
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("messages API: reply is not JSON: %w", err)
	}
	if r.StopReason == "refusal" {
		return "", fmt.Errorf("messages API: the model declined")
	}
	var text strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("messages API: empty answer (stop %q)", r.StopReason)
	}
	return text.String(), nil
}
