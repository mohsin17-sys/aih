package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// commandTimeout bounds every command execution.
const commandTimeout = 30 * time.Second

// maxResultLines bounds what enters the model's context window.
const maxResultLines = 40

// readOnlyCommands are safe to run without any approval: they cannot modify
// state. Add more as needed (keep it conservative).
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true,
	"df": true, "du": true, "free": true, "uptime": true, "pwd": true,
	"whoami": true, "date": true, "uname": true, "echo": true,
	"which": true, "file": true, "stat": true, "env": true,
	"git": true, // status/log/diff are the common cases; see note below
}

// hardBlockPatterns are refused even if the user approves. These are the
// "no second chances" class: unrecoverable destruction.
var hardBlockPatterns = []string{
	"rm -rf /", "rm -rf ~", "rm -rf /*",
	"mkfs", "dd of=/dev/", "> /dev/sd",
	":(){ :|:& };:", "chmod -R 777 /",
	"shutdown", "reboot", "init 0", "init 6",
}

// isReadOnly reports whether every command in a (possibly chained) line is
// allowlisted. Chains (&&, ;, |) require ALL parts to be read-only.
func isReadOnly(cmdStr string) bool {
	for _, part := range strings.FieldsFunc(cmdStr, func(r rune) bool {
		return r == ';' || r == '|' || r == '&' || r == '\n'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// strip leading env-var assignments (FOO=bar cmd)
		fields := strings.Fields(part)
		i := 0
		for i < len(fields) && strings.Contains(fields[i], "=") {
			i++
		}
		if i >= len(fields) {
			return false
		}
		if !readOnlyCommands[fields[i]] {
			return false
		}
	}
	return true
}

// isHardBlocked reports whether a command matches an unrecoverable pattern.
func isHardBlocked(cmdStr string) bool {
	low := strings.ToLower(cmdStr)
	for _, p := range hardBlockPatterns {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func RunCommandTool() *ToolHandler {
	return &ToolHandler{
		Name: "run_command",
		Description: "Run a shell command in the working directory (30s timeout). Returns exit code, duration, and combined output truncated to the first 40 lines. Read-only commands (ls, cat, df, git status, ...) run without approval; all others require user approval; destructive patterns are always refused.",
		Parameters: map[string]any{
			"command": map[string]any{"type": "string", "description": "The shell command to run"},
		},
		Required: []string{"command"},

		// Sensitive is decided per-command at Run time via the sentinel:
		// see the check below. Non-read-only commands are marked sensitive
		// by the agent loop through ShouldPrompt(); this base handler uses
		// dynamic sensitivity handled in Run.
		Sensitive: true,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			cmdStr, _ := args["command"].(string)
			cmdStr = strings.TrimSpace(cmdStr)
			if cmdStr == "" {
				return "", fmt.Errorf("empty command")
			}

			if isHardBlocked(cmdStr) {
				return "", fmt.Errorf("refused: command matches a hard-blocked destructive pattern and cannot be run even with approval; choose a safer approach")
			}

			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			defer cancel()

			start := time.Now()
			cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
			cmd.Dir = tc.WorkDir
			var buf bytes.Buffer
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			err := cmd.Run()
			dur := time.Since(start).Round(time.Millisecond)

			out := truncateLines(buf.String(), maxResultLines)

			var exitNote string
			switch {
			case ctx.Err() == context.DeadlineExceeded:
				exitNote = " (timed out after 30s; partial output above)"
			case err != nil:
				if ee, ok := err.(*exec.ExitError); ok {
					exitNote = fmt.Sprintf(" (exit code %d)", ee.ExitCode())
				} else {
					return out, fmt.Errorf("command failed to start: %v", err)
				}
			default:
				exitNote = " (exit code 0)"
			}

			if out == "" {
				out = "(no output)"
			}
			return fmt.Sprintf("%s [%s]%s", out, dur, exitNote), nil
		},
	}
}

// ShouldPrompt reports whether a command needs interactive approval.
// Exported so the agent loop can mark sensitivity dynamically.
func ShouldPrompt(command string) bool {
	return !isReadOnly(command)
}

// truncateLines keeps the first n lines, rune-safe.
func truncateLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	kept := strings.Join(lines[:n], "\n")
	if !utf8.ValidString(kept) {
		kept = strings.ToValidUTF8(kept, "")
	}
	return kept + "\n...[" + fmt.Sprint(len(lines)-n) + " more lines truncated]"
}
