package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	Model   string
	Key     string
	BaseURL string
	HTTP    *http.Client
}

// ChatProvider is the planner-facing interface: any client that can run
// one tool-calling chat round. Mistral cloud, mlx-serve, Ollama etc. all
// satisfy it — the agent loop does not care where the planner runs.
type ChatProvider interface {
	Chat(ctx context.Context, msgs []Message, tools []Tool) (*ChatResult, error)
}

func NewMistral(model string) *Mistral {
	return &Mistral{
		Model:   model,
		Key:     os.Getenv("MISTRAL_API_KEY"),
		BaseURL: "https://api.mistral.ai/v1",
		HTTP:    streamHTTPClient(),
	}
}

// NewOpenAICompat builds a chat client for any OpenAI-compatible server
// (mlx-serve, MTPLX, Ollama, llama.cpp, vLLM, ...) — the wire protocol is
// the same one the Mistral client speaks. apiKey may be empty when the
// server requires no authentication (the Authorization header is then
// omitted).
func NewOpenAICompat(baseURL, model, apiKey string) *Mistral {
	return &Mistral{
		Model:   model,
		Key:     apiKey,
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		HTTP:    streamHTTPClient(),
	}
}

// streamHTTPClient bounds only connection phases (dial, TLS, headers).
// The response body is consumed as it is generated (SSE), so a
// whole-request timeout would kill long thinking generations mid-stream;
// the total planner exchange is capped by plannerTimeout instead.
func streamHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 300 * time.Second,
		},
	}
}

// plannerTimeout caps one whole planner exchange. Thinking local models
// generate for a long time before the first byte of the answer, so the
// default is generous (15m). FRIDAY_PLANNER_TIMEOUT=<secs> overrides;
// <= 0 disables the cap.
func plannerTimeout() time.Duration {
	if v := os.Getenv("FRIDAY_PLANNER_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 0 {
				return 0
			}
			return time.Duration(n) * time.Second
		}
	}
	return 900 * time.Second
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
		res, err := m.chatOnce(ctx, msgs, tools)
		if err == nil {
			return res, nil
		}
		if !retryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%s: %d attempts failed: %w", m.label(), attempts, lastErr)
}

// label names the server in error messages ("mistral" for the cloud API,
// otherwise the model-neutral "planner").
func (m *Mistral) label() string {
	if m.BaseURL == "https://api.mistral.ai/v1" {
		return "mistral"
	}
	return "planner"
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
		if strings.Contains(err.Error(), fmt.Sprintf("HTTP %d:", code)) {
			return true
		}
	}
	return false
}

func (m *Mistral) chatOnce(ctx context.Context, msgs []Message, tools []Tool) (*ChatResult, error) {
	// Stream: headers arrive immediately and tokens flow as they are
	// generated, so a thinking model that reasons for minutes never
	// stalls the request waiting for the full response.
	payload := map[string]any{
		"model":          m.Model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	body, _ := json.Marshal(payload)
	if os.Getenv("FRIDAY_DEBUG") == "1" {
		os.WriteFile("/tmp/friday-debug.json", body, 0644)
	}
	req, _ := http.NewRequest("POST", m.BaseURL+"/chat/completions", bytes.NewBuffer(body))
	if d := plannerTimeout(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	req = req.WithContext(ctx)
	if m.Key != "" {
		req.Header.Set("Authorization", "Bearer "+m.Key)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		httpErr := fmt.Errorf("%s HTTP %d: %s", m.label(), resp.StatusCode, buf.String())
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
	return m.readStream(resp.Body)
}

// readStream consumes an SSE chat stream and assembles content
// fragments and tool-call deltas into one assistant message. Reasoning
// deltas from thinking models are skipped; usage is taken from whatever
// final chunk carries it.
func (m *Mistral) readStream(body io.Reader) (*ChatResult, error) {
	rd := bufio.NewReaderSize(body, 64*1024)
	var (
		assistant Message
		calls     []ToolCall
		promptTok int
		complTok  int
	)
	assistant.Role = "assistant"
	for {
		line, err := rd.ReadBytes('\n')
		data := bytes.TrimSpace(line)
		if len(data) > 0 && bytes.HasPrefix(data, []byte("data:")) {
			data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte("data:")))
			if bytes.Equal(data, []byte("[DONE]")) {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(data, &chunk) != nil {
				// skip malformed events
			} else {
				for _, ch := range chunk.Choices {
					assistant.Content += ch.Delta.Content
					for _, tc := range ch.Delta.ToolCalls {
						for len(calls) <= tc.Index {
							calls = append(calls, ToolCall{Type: "function"})
						}
						if tc.ID != "" {
							calls[tc.Index].ID = tc.ID
						}
						if tc.Function.Name != "" {
							calls[tc.Index].Function.Name += tc.Function.Name
						}
						calls[tc.Index].Function.Arguments += tc.Function.Arguments
					}
				}
				if chunk.Usage != nil {
					promptTok = chunk.Usage.PromptTokens
					complTok = chunk.Usage.CompletionTokens
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("%s: stream: %w", m.label(), err)
		}
	}
	if assistant.Content == "" && len(calls) == 0 {
		return nil, fmt.Errorf("%s: empty stream", m.label())
	}
	assistant.ToolCalls = calls
	res := &ChatResult{Messages: []Message{assistant}}
	res.Usage.PromptTokens = promptTok
	res.Usage.CompletionTokens = complTok
	return res, nil
}
