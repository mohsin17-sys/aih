package aiharness

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "time"
)

type JevRequest struct {
    Model     string                 `json:"model"`
    State     string                 `json:"state"`
    Questions map[string]JevQuestion `json:"questions"`
}

type JevQuestion struct {
    Type         string            `json:"type"`
    Instructions string            `json:"instructions"`
    Criteria     map[string]string `json:"criteria,omitempty"`
}

type JevAnswer struct {
    Type          string             `json:"type"`
    Choice        string             `json:"choice"`
    Confidence    float64            `json:"confidence"`
    Probabilities map[string]float64 `json:"probabilities"`
}

type JevResponse struct {
    Model   string               `json:"model"`
    Answers map[string]JevAnswer `json:"answers"`
    Usage   struct {
        InputTokens  int `json:"input_tokens"`
        OutputTokens int `json:"output_tokens"`
    } `json:"usage"`
}

// QueryJev asks Jev to decide which MCP tool to use. Returns raw body + status.
func QueryJev(state string, toolOptions []string) (string, int, error) {
    apiKey := os.Getenv("TYPESAFE_API_KEY")
    if apiKey == "" {
        return "", 0, fmt.Errorf("TYPESAFE_API_KEY not set")
    }
    url := os.Getenv("TYPESAFE_API_URL")
    if url == "" {
        url = "https://api.typesafe.ai/v1/systemone"
    }

    criteria := make(map[string]string)
    for _, tool := range toolOptions {
        criteria[tool] = fmt.Sprintf("Use the %s tool", tool)
    }

    payload := JevRequest{
        Model: "jev-latest",
        State: state,
        Questions: map[string]JevQuestion{
            "selected_tool": {
                Type:         "choice",
                Instructions: "Choose the optimal MCP tool to execute this task securely.",
                Criteria:     criteria,
            },
        },
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return "", 0, fmt.Errorf("marshal request: %w", err)
    }
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
    if err != nil {
        return "", 0, fmt.Errorf("build request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+apiKey)
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 60 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return "", 0, fmt.Errorf("call jev: %w", err)
    }
    defer resp.Body.Close()

    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", resp.StatusCode, fmt.Errorf("read response: %w", err)
    }
    return string(respBody), resp.StatusCode, nil
}

// QueryJevDecided calls QueryJev and parses the routing decision.
func QueryJevDecided(state string, toolOptions []string) (*JevResponse, int, error) {
    raw, status, err := QueryJev(state, toolOptions)
    if err != nil {
        return nil, status, err
    }
    var resp JevResponse
    if err := json.Unmarshal([]byte(raw), &resp); err != nil {
        return nil, status, fmt.Errorf("parse jev response: %w (raw: %s)", err, raw)
    }
    return &resp, status, nil
}
