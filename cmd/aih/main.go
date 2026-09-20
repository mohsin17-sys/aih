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
	"aiharness/internal/app"
	"aiharness/internal/providers"
	"aiharness/internal/tui"
)

func main() {
	execMode := flag.String("e", "", "headless: execute a single task and exit")
	maxSteps := flag.Int("steps", 40, "maximum agent steps")
	flag.Parse()

	wd, _ := os.Getwd()

	// TUI mode: live agent, streaming events into the panes
	if *execMode == "" {
		if err := tui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "tui:", err)
			os.Exit(1)
		}
		return
	}

	// ---- headless mode (live streaming to stdout) ----
	ag, err := app.NewAgent(wd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	ag.Messages = append(ag.Messages, providers.Message{Role: "user", Content: *execMode})
	ag.Emit = printEvent

	if err := ag.Run(context.Background(), *maxSteps); err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
	}

	// Persist session JSONL (audit trail)
	sessDir := filepath.Join(wd, ".aih", "sessions")
	os.MkdirAll(sessDir, 0o755)
	sessFile := filepath.Join(sessDir, time.Now().Format("20060102-150405")+".jsonl")
	if f, err := os.Create(sessFile); err == nil {
		enc := json.NewEncoder(f)
		for _, ev := range ag.Events {
			enc.Encode(ev)
		}
		f.Close()
		fmt.Fprintf(os.Stderr, "session: %s\n", sessFile)
	}
}

// printEvent renders one event line as it happens (headless live view).
func printEvent(ev agent.Event) {
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
