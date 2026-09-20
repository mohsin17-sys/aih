// Package app constructs the agent identically for headless and TUI modes.
package app

import (
	"aiharness/config"
	"aiharness/internal/agent"
	"aiharness/internal/providers"
)

// NewAgent builds a fully-equipped Agent for the given working directory.
// The returned agent has only the system prompt in its message history;
// callers append the user turn (headless: the -e task; TUI: per Enter key).
// Approvals follow the config's auto-approve policy (interactive approvals
// arrive in session 3).
func NewAgent(wd string) (*agent.Agent, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	tools := map[string]*agent.ToolHandler{
		"read_file":   agent.ReadFileTool(),
		"list_dir":    agent.ListDirTool(),
		"write_file":  agent.WriteFileTool(),
		"run_command": agent.RunCommandTool(),
	}
	if cfg.Jev.Enabled {
		tools["jev_route"] = agent.JevRouteTool()
	}
	if cfg.Editor.BaseURL != "" {
		mlx := providers.NewMLX(cfg.Editor.BaseURL, cfg.Editor.Model)
		tools["generate_edit"] = agent.GenerateEditTool(mlx)
	}

	return &agent.Agent{
		Planner: providers.NewMistral(cfg.Planner.Model),
		Tools:   tools,
		Approve: func(r agent.ApprovalRequest) bool {
			return (r.Tool == "write_file" && cfg.Safety.AutoApproveWrite) ||
				(r.Tool == "run_command" && cfg.Safety.AutoApproveShell)
		},
		WorkDir:  wd,
		Messages: []providers.Message{{Role: "system", Content: agent.SystemPrompt}},
	}, nil
}
