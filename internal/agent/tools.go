package agent

import (
	"encoding/json"

	"aiharness/internal/providers"
)

type ToolContext struct {
	WorkDir string
	// Emit, when non-nil, lets a tool stream usage/telemetry events.
	Emit func(Event)
}

type ApprovalRequest struct {
	Tool   string
	Args   map[string]any
	Reason string
}

type Approver func(ApprovalRequest) bool

type ToolHandler struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
	Sensitive   bool
	// SensitiveFn, when set, decides per call whether the approval gate
	// applies (for tools whose sensitivity depends on their arguments).
	SensitiveFn func(args map[string]any) bool
	Run         func(tc ToolContext, args map[string]any) (string, error)
}

func (t *ToolHandler) Definition() providers.Tool {
	schema := map[string]any{
		"type":       "object",
		"properties": t.Parameters,
	}
	if len(t.Required) > 0 {
		schema["required"] = t.Required
	}
	raw, _ := json.Marshal(schema)
	return providers.Tool{
		Type: "function",
		Function: providers.ToolFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  raw,
		},
	}
}
