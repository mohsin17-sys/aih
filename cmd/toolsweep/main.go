// toolsweep reads a friday debug JSON payload, extracts the tools array,
// and sends one curl-equivalent API request per tool to identify ALL
// offending tool schemas in one pass.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	// Read the debug JSON
	data, err := os.ReadFile("/tmp/friday-debug.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading /tmp/friday-debug.json: %v\n", err)
		os.Exit(1)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// Extract tools
	toolsAny, ok := payload["tools"]
	if !ok {
		fmt.Fprintf(os.Stderr, "No 'tools' field in payload\n")
		os.Exit(1)
	}

	tools := toolsAny.([]any)
	model := payload["model"].(string)

	// Get API key
	apiKey := os.Getenv("MISTRAL_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "MISTRAL_API_KEY not set\n")
		os.Exit(1)
	}

	client := &http.Client{Timeout: 60 * time.Second}

	fmt.Printf("Testing %d tools against Mistral API...\n\n", len(tools))

	for i, toolAny := range tools {
		tool := toolAny.(map[string]any)
		funcMap := tool["function"].(map[string]any)
		name := funcMap["name"].(string)

		// Create minimal request with just this one tool
		minimalPayload := map[string]any{
			"model":   model,
			"messages": []map[string]any{{"role": "user", "content": "test"}},
			"tools":   []map[string]any{tool},
		}

		body, _ := json.Marshal(minimalPayload)

		req, _ := http.NewRequest("POST", "https://api.mistral.ai/v1/chat/completions", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("✗ Tool %d/%d: %s - Request failed: %v\n", i+1, len(tools), name, err)
			continue
		}

		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode == 200 {
			fmt.Printf("✓ Tool %d/%d: %s - OK\n", i+1, len(tools), name)
		} else {
			var errResult map[string]any
			json.Unmarshal(respBody, &errResult)
			
			msg := errResult["message"].(string)
			typ := errResult["type"].(string)
			
			fmt.Printf("✗ Tool %d/%d: %s\n", i+1, len(tools), name)
			fmt.Printf("  Status: %d\n", resp.StatusCode)
			fmt.Printf("  Type: %s\n", typ)
			fmt.Printf("  Message: %s\n", msg)
			fmt.Println()
		}

		// Rate limit to avoid hitting API limits
		time.Sleep(100 * time.Millisecond)
	}
}
