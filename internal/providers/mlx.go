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

func (m *MLX) Complete(system, user string) (string, error) {
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
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return "", fmt.Errorf("mlx HTTP %d: %s", resp.StatusCode, buf.String())
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("mlx: no choices")
	}
	return out.Choices[0].Message.Content, nil
}
