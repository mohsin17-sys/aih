package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Mistral struct {
	Model string
	Key   string
	HTTP  *http.Client
}

func NewMistral(model string) *Mistral {
	return &Mistral{
		Model: model,
		Key:   os.Getenv("MISTRAL_API_KEY"),
		HTTP:  &http.Client{Timeout: 120 * time.Second},
	}
}

type ChatResult struct {
	Messages []Message
	Usage    struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	}
}

// Chat retries transient failures: network errors and 429/5xx.
// Auth/validation errors (400/401/403/404/422) fail fast.
func (m *Mistral) Chat(ctx context.Context, msgs []Message, tools []Tool) (*ChatResult, error) {
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// respect cancellation: never sleep-and-retry after Ctrl-C
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			wait := time.Duration(1<<uint(i)) * time.Second // 2s, 4s
			if ra, ok := lastErr.(*retryAfterError); ok && ra.after > 0 {
				wait = ra.after
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		res, err := m.chatOnce(msgs, tools)
		if err == nil {
			return res, nil
		}
		if !retryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("mistral: %d attempts failed: %w", attempts, lastErr)
}

type retryAfterError struct {
	after time.Duration
	err   error
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*retryAfterError); ok {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"remote error", "connection reset", "broken pipe", "eof", "timeout", "tls"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	for _, code := range []int{429, 500, 502, 503, 504} {
		if strings.Contains(err.Error(), fmt.Sprintf("mistral HTTP %d:", code)) {
			return true
		}
	}
	return false
}

func (m *Mistral) chatOnce(msgs []Message, tools []Tool) (*ChatResult, error) {
	payload := map[string]any{"model": m.Model, "messages": msgs}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	body, _ := json.Marshal(payload)
	if os.Getenv("FRIDAY_DEBUG") == "1" {
		os.WriteFile("/tmp/friday-debug.json", body, 0644)
	}
	req, _ := http.NewRequest("POST", "https://api.mistral.ai/v1/chat/completions", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+m.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		httpErr := fmt.Errorf("mistral HTTP %d: %s", resp.StatusCode, buf.String())
		if resp.StatusCode == 429 {
			after := 4 * time.Second
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, perr := strconv.Atoi(ra); perr == nil {
					after = time.Duration(secs) * time.Second
				}
			}
			return nil, &retryAfterError{after: after, err: httpErr}
		}
		return nil, httpErr
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("mistral: no choices returned")
	}
	return &ChatResult{Messages: []Message{out.Choices[0].Message}, Usage: out.Usage}, nil
}
