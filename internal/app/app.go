// Package app constructs the agent identically for headless and TUI modes.
package app

import (
	"aiharness/config"
	"aiharness/internal/agent"
	"aiharness/internal/providers"
)

// NewAgent builds a fully-equipped Agent for the given working directory.
// approver: if non-nil, it is used for every write/command approval (the TUI
// passes an interactive y/n prompt). If nil, the config auto-approve policy
// applies (headless behavior, unchanged).
func NewAgent(wd string, approver func(agent.ApprovalRequest) bool) (*agent.Agent, error) {
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

	ag := &agent.Agent{
		Planner:  providers.NewMistral(cfg.Planner.Model),
		Tools:    tools,
		WorkDir:  wd,
		Messages: []providers.Message{{Role: "system", Content: agent.SystemPrompt}},
	}

	if approver != nil {
		ag.Approve = approver
	} else {
		ag.Approve = func(r agent.ApprovalRequest) bool {
			return (r.Tool == "write_file" && cfg.Safety.AutoApproveWrite) ||
				(r.Tool == "run_command" && cfg.Safety.AutoApproveShell)
		}
	}
	return ag, nil
}
