package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aiharness/internal/agent"
	"aiharness/config"
	"aiharness/internal/providers"
)

func main() {
	execMode := flag.String("e", "", "headless: execute a single task and exit")
	maxSteps := flag.Int("steps", 40, "maximum agent steps")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	if *execMode == "" {
		fmt.Println("aih: TUI not implemented yet — use: aih -e \"task\"")
		return
	}

	wd, _ := os.Getwd()

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
		Planner: providers.NewMistral(cfg.Planner.Model),
		Tools:   tools,
		Approve: func(r agent.ApprovalRequest) bool {
			return (r.Tool == "write_file" && cfg.Safety.AutoApproveWrite) ||
				(r.Tool == "run_command" && cfg.Safety.AutoApproveShell)
		},
		WorkDir: wd,
		Messages: []providers.Message{
			{Role: "system", Content: agent.SystemPrompt},
			{Role: "user", Content: *execMode},
		},
	}

	if err := ag.Run(context.Background(), *maxSteps); err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
	}

	// Print transcript
	for _, ev := range ag.Events {
		switch ev.Type {
		case "tool_call":
			fmt.Printf("→ %s %s\n", ev.Tool, compactArgs(ev.Args))
		case "tool_result":
			fmt.Printf("← %s\n", firstLine(ev.Result))
		case "error":
			fmt.Printf("✗ %s: %s\n", ev.Tool, firstLine(ev.Result))
		case "assistant":
			fmt.Printf("\n%s\n", ev.Content)
		}
	}

	// Persist session JSONL (audit trail)
	sessDir := filepath.Join(wd, ".aih", "sessions")
	os.MkdirAll(sessDir, 0o755)
	sessFile := filepath.Join(sessDir, time.Now().Format("20060102-150405")+".jsonl")
	f, err := os.Create(sessFile)
	if err == nil {
		enc := json.NewEncoder(f)
		for _, ev := range ag.Events {
			enc.Encode(ev)
		}
		f.Close()
		fmt.Fprintf(os.Stderr, "session: %s\n", sessFile)
	}
}

func compactArgs(args map[string]any) string {
	b, _ := json.Marshal(args)
	s := string(b)
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}
