package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type MLX struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func NewMLX(baseURL, model string) *MLX {
	return &MLX{BaseURL: baseURL, Model: model, HTTP: &http.Client{Timeout: 180 * time.Second}}
}

// MLXResult carries content plus telemetry from one completion.
type MLXResult struct {
	Content         string
	PromptTokens    int
	CompletionToken int
	Duration        time.Duration
}

// CompleteWithUsage runs one completion and returns content + usage + latency.
func (m *MLX) CompleteWithUsage(system, user string) (*MLXResult, error) {
	payload := map[string]any{
		"model": m.Model,
		"messages": []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"max_tokens":  8192,
		"temperature": 0.1,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", m.BaseURL+"/chat/completions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dur := time.Since(start).Round(time.Millisecond)

	if resp.StatusCode != 200 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("mlx HTTP %d: %s", resp.StatusCode, buf.String())
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
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("mlx: no choices")
	}
	return &MLXResult{
		Content:         out.Choices[0].Message.Content,
		PromptTokens:    out.Usage.PromptTokens,
		CompletionToken: out.Usage.CompletionTokens,
		Duration:        dur,
	}, nil
}

// Complete is the plain-content wrapper (kept for compatibility).
func (m *MLX) Complete(system, user string) (string, error) {
	r, err := m.CompleteWithUsage(system, user)
	if err != nil {
		return "", err
	}
	return r.Content, nil
}

// Healthy pings the server's /v1/models endpoint (mlx-lm serves this).
func (m *MLX) Healthy() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(m.BaseURL + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
