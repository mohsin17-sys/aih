package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"os"
	"fmt"

	"aiharness/internal/providers"
)

const SystemPrompt = `You are AIH, an autonomous coding agent working in a real repository.
You operate through tool calls only — never describe changes, execute them.
Rules:
- read_file before writing or discussing any file; never guess file contents
- for large file bodies (>200 lines), call generate_edit to produce content, then write_file to save it
- before run_command with side effects, call jev_route and include its decision
- work incrementally; after each step, verify with read_file or run_command
- when the task is complete, reply with a short summary and no tool calls`

type Agent struct {
	Planner  *providers.Mistral
	Tools    map[string]*ToolHandler
	Approve  Approver
	WorkDir  string
	Messages []providers.Message
	Events   []Event

	// Emit, when non-nil, is called for every event as it happens
	// (in addition to appending to Events). The TUI uses this to
	// stream tool calls live; headless mode prints from it.
	Emit func(Event)

	// Session telemetry
	TokensIn  int `json:"tokens_in"`
	TokensOut int `json:"tokens_out"`
}

// record appends an event and streams it if an emitter is attached.
func (a *Agent) record(ev Event) {
	a.Events = append(a.Events, ev)
	if a.Emit != nil {
		a.Emit(ev)
	}
}

type Event struct {
	Type    string `json:"type"`
	Tool    string `json:"tool,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Result  string `json:"result,omitempty"`
	Content string `json:"content,omitempty"`
}

func (a *Agent) Step(ctx context.Context) (done bool, err error) {
	var tools []providers.Tool
	for _, t := range a.Tools {
		tools = append(tools, t.Definition())
	}
	res, err := a.Planner.Chat(ctx, a.Messages, tools)
	if err != nil {
		return false, err
	}
	a.TokensIn += res.Usage.PromptTokens
	a.TokensOut += res.Usage.CompletionTokens
	a.record(Event{Type: "usage", Tool: "planner", Args: map[string]any{
		"in": res.Usage.PromptTokens, "out": res.Usage.CompletionTokens}})
	a.Messages = append(a.Messages, res.Messages...)

	assistantMsg := res.Messages[0]
	if len(assistantMsg.ToolCalls) == 0 {
		a.record(Event{Type: "assistant", Content: assistantMsg.Content})
		return true, nil
	}

	for _, call := range assistantMsg.ToolCalls {
		var args map[string]any
		json.Unmarshal([]byte(call.Function.Arguments), &args)
		a.record(Event{Type: "tool_call", Tool: call.Function.Name, Args: args})

		result, err := a.runTool(ctx, call.Function.Name, args)
		msg := providers.Message{
			Role:       "tool",
			ToolCallID: call.ID,
			Name:       call.Function.Name,
		}
		if err != nil {
			msg.Content = "ERROR: " + err.Error()
			a.record(Event{Type: "error", Tool: call.Function.Name, Result: err.Error()})
		} else {
			msg.Content = a.trimResult(result)
			a.record(Event{Type: "tool_result", Tool: call.Function.Name, Result: result})
		}
		a.Messages = append(a.Messages, msg)
	}
	return false, nil
}

func (a *Agent) Run(ctx context.Context, maxSteps int) error {
	for i := 0; i < maxSteps; i++ {
		done, err := a.Step(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return fmt.Errorf("max steps (%d) reached", maxSteps)
}

func (a *Agent) runTool(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := a.Tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	sensitive := t.Sensitive
	if name == "run_command" {
		if cmd, ok := args["command"].(string); ok {
			sensitive = ShouldPrompt(cmd)
		}
	}
	if sensitive && a.Approve != nil {
		if !a.Approve(ApprovalRequest{Tool: name, Args: args}) {
			return "", fmt.Errorf("denied by user — do not retry this or similar commands; continue with non-sensitive tools (read-only shell commands and file reads are always available) or state the limitation in your final answer")
		}
	}
	return t.Run(ToolContext{WorkDir: a.WorkDir, Emit: a.record}, args)
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// trimResult ages tool results out of the MODEL context (the audit
// log keeps full text). Once the conversation exceeds the round
// threshold, result bodies are capped to stop quadratic token growth.
func (a *Agent) trimResult(s string) string {
	if len(a.Messages) <= envInt("FRIDAY_CTX_MAXMSGS", 12) {
		return s
	}
	cap := envInt("FRIDAY_CTX_RESULTCAP", 500)
	if len(s) <= cap {
		return s
	}
	return s[:cap] + "\n…[truncated; full text preserved in session log]"
}
