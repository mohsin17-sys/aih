package agent

import (
	"fmt"
	"sort"
	"strings"

	"aiharness"
)

func JevRouteTool() *ToolHandler {
	return &ToolHandler{
		Name: "jev_route",
		Description: "Consult the Jev orchestrator on which MCP tool should handle a task. Returns the selected tool, confidence, and probability distribution. Call this BEFORE run_command for external actions (docker, github, scraping, storage) and record the decision.",
		Parameters: map[string]any{
			"query": map[string]any{"type": "string", "description": "Natural-language description of the task"},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Candidate tools, e.g. [\"filesystem\",\"docker\"]",
			},
		},
		Required:  []string{"query", "tools"},
		Sensitive: false,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			toolsRaw, _ := args["tools"].([]any)
			var tools []string
			for _, t := range toolsRaw {
				if s, ok := t.(string); ok {
					tools = append(tools, s)
				}
			}
			resp, status, err := aiharness.QueryJevDecided(query, tools)
			if err != nil {
				return "", err
			}
			if status < 200 || status >= 300 {
				return "", fmt.Errorf("jev HTTP %d", status)
			}
			ans := resp.Answers["selected_tool"]
			var probs []string
			for t, p := range ans.Probabilities {
				probs = append(probs, fmt.Sprintf("%s=%.2f", t, p))
			}
			sort.Strings(probs)
			return fmt.Sprintf("Jev (%s) selected tool: %s (confidence %.2f). Probabilities: %s",
				resp.Model, ans.Choice, ans.Confidence, strings.Join(probs, " ")), nil
		},
	}
}
