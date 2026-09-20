package agent

import (
	"bytes"
	"fmt"
	"os/exec"
)

func RunCommandTool() *ToolHandler {
	return &ToolHandler{
		Name: "run_command",
		Description: "Run a shell command in the working directory. Combined stdout+stderr is returned (truncated to 10k). Requires user approval unless auto-approved in config.",
		Parameters: map[string]any{
			"command": map[string]any{"type": "string", "description": "The shell command to run"},
		},
		Required:  []string{"command"},
		Sensitive: true,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			cmdStr, _ := args["command"].(string)
			cmd := exec.Command("bash", "-c", cmdStr)
			cmd.Dir = tc.WorkDir
			var buf bytes.Buffer
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			if err := cmd.Run(); err != nil {
				return buf.String(), fmt.Errorf("command failed: %w (output: %s)", err, truncate(buf.String(), 2000))
			}
			return truncate(buf.String(), 10_000), nil
		},
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}
