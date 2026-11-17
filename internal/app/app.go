// Package app constructs the agent identically for headless and TUI modes.
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"aiharness/config"
	"aiharness/internal/agent"
	"aiharness/internal/mcp"
	"aiharness/internal/providers"
)

// toolValidationResult holds the result of validating one tool's schema.
type toolValidationResult struct {
	name    string
	valid   bool
	errMsg  string
}

// validateSchema checks if a tool's schema is valid for Mistral's API.
// Returns an error message if invalid, empty string if valid.
func validateSchema(toolDef providers.Tool) string {
	// Marshal the tool definition to check its structure
	body, err := json.Marshal(toolDef)
	if err != nil {
		return err.Error()
	}
	
	// Unmarshal to check structure
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return err.Error()
	}
	
	// Check that function exists and is valid
	funcMap, ok := parsed["function"].(map[string]any)
	if !ok {
		return "missing or invalid 'function' field"
	}
	
	// Check parameters exists and is valid
	paramsRaw, ok := funcMap["parameters"].(map[string]any)
	if !ok {
		// parameters might be json.RawMessage which unmarshaled as map or other type
		// Try to check if it's at least present
		if _, hasParams := funcMap["parameters"]; !hasParams {
			return "missing 'parameters' field in function"
		}
		// If we can't parse it, that's okay - it might be valid raw JSON
		return ""
	}
	
	// Check for nested properties (the double-wrapping issue)
	if props, ok := paramsRaw["properties"].(map[string]any); ok {
		// Check if properties itself has a properties field (double nesting)
		if _, hasNestedProps := props["properties"]; hasNestedProps {
			return "nested 'properties' detected in schema"
		}
		// Also check that properties doesn't have type: "object" at this level
		// (it should only have property definitions)
		if typ, hasType := props["type"]; hasType {
			if t, ok := typ.(string); ok && t == "object" {
				return "'properties' should not have type: object"
			}
		}
		// Check if properties has required at this level (should be at parent level)
		if _, hasRequired := props["required"]; hasRequired {
			// This might be okay depending on context
		}
	}
	
	return ""
}

// validateTools validates all tools' schemas locally.
// Tools that fail validation are removed from the tools map and a warning is printed.
func validateTools(tools map[string]*agent.ToolHandler) {
	// Channel for results
	ch := make(chan toolValidationResult, len(tools))
	
	// Use a wait group to wait for all validations
	var wg sync.WaitGroup
	
	for name, tool := range tools {
		wg.Add(1)
		go func(name string, tool *agent.ToolHandler) {
			defer wg.Done()
			
			// Validate the schema locally
			if errMsg := validateSchema(tool.Definition()); errMsg != "" {
				ch <- toolValidationResult{name: name, valid: false, errMsg: errMsg}
				return
			}
			
			ch <- toolValidationResult{name: name, valid: true, errMsg: ""}
		}(name, tool)
	}
	
	// Close channel when all validations are done
	go func() {
		wg.Wait()
		close(ch)
	}()
	
	// Process results and disable invalid tools
	for result := range ch {
		if !result.valid {
			delete(tools, result.name)
			fmt.Fprintf(os.Stderr, "friday: tool %s disabled: schema validation failed: %s\n", result.name, result.errMsg)
		}
	}
}

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

	// MCP servers: connected in parallel with an overall budget;
	// failures and timeouts are non-fatal — one bad server must
	// never kill (or stall) the session. Servers marked
	// enabled = false in config are skipped entirely.
	type mcpResult struct {
		name string
		srv  *mcp.Server
		err  error
	}
	ch := make(chan mcpResult, len(cfg.MCP.Servers))
	launched := 0
	for name, s := range cfg.MCP.Servers {
		if s.Command == "" {
			continue
		}
		if s.Enabled != nil && !*s.Enabled {
			continue
		}
		launched++
		go func(name, command string, args, env []string, trusted bool) {
			srv := &mcp.Server{Name: name, Command: command, Args: args, Env: env, Trusted: trusted}
			ch <- mcpResult{name, srv, srv.Connect()}
		}(name, s.Command, s.Args, s.Env, s.Trusted)
	}
	var servers []*mcp.Server
	var mcpUp, mcpDown []string
	budget := time.After(45 * time.Second)
	for i := 0; i < launched; i++ {
		select {
			case r := <-ch:
				if r.err != nil {
					fmt.Fprintf(os.Stderr, "friday: mcp %s disabled: %v\n", r.name, r.err)
					mcpDown = append(mcpDown, r.name)
					continue
				}
				mcpUp = append(mcpUp, fmt.Sprintf("%s (%d)", r.name, len(r.srv.Tools())))
				servers = append(servers, r.srv)
			case <-budget:
				fmt.Fprintf(os.Stderr, "friday: mcp connect budget exceeded, continuing without remaining servers\n")
				goto done
		}
	}
done:
	// Expose every connected server through the dispatch pair
	// (mcp_list_tools / mcp_call) instead of one schema per remote
	// tool: the per-request prompt stays constant no matter how many
	// tools the servers advertise.
	for k, v := range agent.MCPDispatchTools(servers) {
		tools[k] = v
	}

	// Pre-flight: validate every tool's schema.
	// Tools that fail validation are disabled with a stderr warning.
	// This runs in parallel to avoid slowing down startup.
	if len(tools) > 0 {
		validateTools(tools)
	}

	// Planner: an OpenAI-compatible server when [planner].base_url is set
	// (e.g. mlx-serve on a Mac mini, Ollama, llama.cpp); otherwise the
	// Mistral cloud API. Any OpenAI-compatible endpoint works — the wire
	// protocol (tool_calls, usage) is identical.
	var planner providers.ChatProvider
	if cfg.Planner.BaseURL != "" {
		planner = providers.NewOpenAICompat(cfg.Planner.BaseURL, cfg.Planner.Model, cfg.Planner.APIKey)
	} else {
		planner = providers.NewMistral(cfg.Planner.Model)
	}

	ag := &agent.Agent{
		Planner:     planner,
		Tools:       tools,
		WorkDir:     wd,
		Messages:    []providers.Message{{Role: "system", Content: agent.BuildSystemPrompt(cfg.Jev.Enabled, len(servers))}},
		MCPServers:  mcpUp,
		MCPDown:     mcpDown,
	}

	// Terminate the MCP child processes when the session ends.
	ag.Shutdown = func() {
		for _, srv := range servers {
			srv.Close()
		}
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
