package agent

import (
	"encoding/json"

	"aiharness/internal/mcp"
)

// MCPTools converts a connected MCP server's tools into ToolHandlers.
//
// Safety: every MCP tool is Sensitive by default (hits the approval gate),
// because an MCP server is arbitrary configured code. Servers marked
// trusted in config are exempted.
func MCPTools(srv *mcp.Server) map[string]*ToolHandler {
	out := map[string]*ToolHandler{}
	for _, t := range srv.Tools() {
		sanitized := SanitizeSchema(t.InputSchema)
		// Extract properties and required from the sanitized schema
		// The MCP InputSchema is already a complete schema object, so we need to
		// pull out the properties map and required list for ToolHandler
		params := map[string]any{}
		var required []string
		if props, ok := sanitized["properties"].(map[string]any); ok {
			params = props
		}
		if req, ok := sanitized["required"].([]string); ok {
			required = req
		}
		tool := &ToolHandler{
			Name:        "mcp_" + srv.Name + "_" + t.Name,
			Description: "(" + srv.Name + ") " + t.Description,
			Parameters:  params,
			Required:    required,
			Sensitive:   !srv.Trusted,
			Run: func(tc ToolContext, args map[string]any) (string, error) {
				res, err := srv.Call(t.Name, args)
				if err != nil {
					return "", err
				}
				return res, nil
			},
		}
		out[tool.Name] = tool
	}
	return out
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
