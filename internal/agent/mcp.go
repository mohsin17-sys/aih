package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"aiharness/internal/mcp"
)

// MCPDispatchTools exposes connected MCP servers through two generic tools
// (mcp_list_tools and mcp_call) instead of one definition per remote tool.
// One schema per remote tool dominated the per-request prompt (measured:
// 38 MCP schemas cost ~8k tokens every round); the dispatch pair keeps that
// cost constant regardless of how many tools the servers advertise.
//
// Safety: calls to servers not marked trusted in config require approval
// (decided per call, since the server is only known at Run time).
func MCPDispatchTools(servers []*mcp.Server) map[string]*ToolHandler {
	byName := map[string]*mcp.Server{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	list := &ToolHandler{
		Name:        "mcp_list_tools",
		Description: "List the tools available on the connected MCP servers. For each tool: server name, tool name, one-line description, and required parameter names. Call this before mcp_call.",
		Parameters: map[string]any{
			"server": map[string]any{"type": "string", "description": "Optional: restrict the listing to one MCP server by name"},
		},
		Sensitive: false,
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			filter, _ := args["server"].(string)
			var b strings.Builder
			for _, s := range servers {
				if filter != "" && s.Name != filter {
					continue
				}
				// Live listing: servers like Serena expose more tools
				// mid-session (e.g. after project activation); on error
				// fall back to the cached list.
				tools, err := s.ListTools()
				if err != nil && len(tools) == 0 {
					fmt.Fprintf(&b, "%s: unavailable (%v)\n", s.Name, err)
					continue
				}
				for _, t := range tools {
					fmt.Fprintf(&b, "%s | %s | %s | required: %s\n",
						s.Name, t.Name, firstLine(t.Description), strings.Join(requiredParams(t.InputSchema), ", "))
				}
			}
			if b.Len() == 0 {
				return "no MCP tools available", nil
			}
			return b.String(), nil
		},
	}
	call := &ToolHandler{
		Name:        "mcp_call",
		Description: "Call one MCP tool. Use mcp_list_tools first to discover the server, tool name and required parameters; pass arguments as a JSON object matching that tool's parameters.",
		Parameters: map[string]any{
			"server":    map[string]any{"type": "string", "description": "MCP server name (from mcp_list_tools)"},
			"tool":      map[string]any{"type": "string", "description": "Tool name on that server"},
			"arguments": map[string]any{"type": "object", "description": "Tool arguments as a JSON object"},
		},
		Required: []string{"server", "tool", "arguments"},
		// Untrusted servers keep the approval gate; the server is only
		// known once the model supplies it, so sensitivity is per call.
		SensitiveFn: func(args map[string]any) bool {
			s := byName[serverArg(args)]
			return s == nil || !s.Trusted
		},
		Run: func(tc ToolContext, args map[string]any) (string, error) {
			srvName := serverArg(args)
			toolName, _ := args["tool"].(string)
			s := byName[srvName]
			if s == nil {
				return "", fmt.Errorf("unknown MCP server %q — call mcp_list_tools to see the available servers", srvName)
			}
			var toolArgs map[string]any
			if v, ok := args["arguments"]; ok && v != nil {
				toolArgs, ok = v.(map[string]any)
				if !ok {
					return "", fmt.Errorf("arguments must be a JSON object")
				}
			}
			// The cached list may predate a mid-session toolset change
			// (Serena swaps in the full toolset after project activation),
			// so refresh once when the name is unknown before delegating
			// the final say to the server itself.
			if !knownTool(s, toolName) {
				s.ListTools()
			}
			return s.Call(toolName, toolArgs)
		},
	}
	return map[string]*ToolHandler{"mcp_list_tools": list, "mcp_call": call}
}

func serverArg(args map[string]any) string {
	name, _ := args["server"].(string)
	return name
}

// knownTool reports whether the server's cached tool list contains name.
func knownTool(s *mcp.Server, name string) bool {
	for _, t := range s.Tools() {
		if t.Name == name {
			return true
		}
	}
	return false
}

// requiredParams pulls the required parameter names out of a raw inputSchema.
func requiredParams(raw json.RawMessage) []string {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return s.Required
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// sanitizeSchema converts an MCP inputSchema into strict JSON-Schema
// that Mistral's tool validation accepts — recursively. Only known-safe
// keys are kept (type, properties, required, description, enum, items);
// $schema, additionalProperties, anyOf unions, format/title/default and
// all other exotic keys are stripped at every nesting level.
func SanitizeSchema(raw json.RawMessage) map[string]any {
	var in any
	if err := json.Unmarshal(raw, &in); err != nil || in == nil {
		return map[string]any{"type": "object"}
	}
	if m, ok := clean(in).(map[string]any); ok && m != nil {
		if _, ok := m["type"]; !ok {
			m["type"] = "object"
		}
		return m
	}
	return map[string]any{"type": "object"}
}

// clean recursively whitelists a schema value. Non-conforming nodes
// (strings, arrays, anything not a schema object) are dropped.
func clean(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, k := range []string{"type", "description", "enum"} {
		if val, ok := m[k]; ok {
			if k == "type" {
				if s, ok := val.(string); ok && s != "" {
					out[k] = s
				}
			} else {
				out[k] = val
			}
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		cp := map[string]any{}
		for pk, pv := range props {
			if c := clean(pv); c != nil {
				cp[pk] = c
			}
		}
		out["properties"] = cp
	}
	if items, ok := m["items"]; ok {
		if c := clean(items); c != nil {
			out["items"] = c
		}
	}
	if req, ok := m["required"].([]any); ok {
		var names []string
		for _, r := range req {
			if s, ok := r.(string); ok {
				names = append(names, s)
			}
		}
		if names != nil {
			out["required"] = names
		}
	}
	return out
}
