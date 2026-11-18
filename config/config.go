package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Planner struct {
		// Provider is informational: any non-empty BaseURL switches the
		// planner to that OpenAI-compatible server; an empty BaseURL
		// falls back to the Mistral cloud API with MISTRAL_API_KEY.
		Provider string `toml:"provider"`
		Model    string `toml:"model"`
		BaseURL  string `toml:"base_url"`
		APIKey   string `toml:"api_key"`
	} `toml:"planner"`
	Editor struct {
		Provider string `toml:"provider"`
		BaseURL  string `toml:"base_url"`
		Model    string `toml:"model"`
	} `toml:"editor"`
	Jev struct {
		Enabled bool `toml:"enabled"`
		// BaseURL/Model/APIKey configure the System One routing endpoint
		// the jev_route tool calls. They map to the TYPESAFE_* env vars the
		// client reads, so a local laya-serve is a config-only switch:
		//   base_url = "http://127.0.0.1:8000/v1/systemone"
		//   model    = "english"
		// Leave BaseURL empty to use hosted TypeSafe Jev (needs TYPESAFE_API_KEY).
		BaseURL string `toml:"base_url"`
		Model   string `toml:"model"`
		APIKey  string `toml:"api_key"`
	} `toml:"jev"`
	Safety struct {
		AutoApproveWrite bool    `toml:"auto_approve_write"`
		AutoApproveShell bool    `toml:"auto_approve_shell"`
		// AutoApproveAll removes every approval gate in both the TUI
		// and headless mode: writes, shell commands, and calls to
		// untrusted MCP servers all execute without prompting. Only the
		// hard-blocked destructive command patterns remain refused.
		AutoApproveAll bool    `toml:"auto_approve_all"`
		MaxCostUSD     float64 `toml:"max_cost_usd"`
	} `toml:"safety"`
	MCP struct {
		Servers map[string]struct {
			Command string   `toml:"command"`
			Args    []string `toml:"args"`
			Env     []string `toml:"env"`
			Trusted bool     `toml:"trusted"`
			// Enabled defaults to true; set enabled = false to skip a
			// server entirely (not spawned, not exposed to the model).
			Enabled *bool `toml:"enabled"`
		} `toml:"servers"`
	} `toml:"mcp"`
	UI struct {
		Theme string `toml:"theme"`
	} `toml:"ui"`
}

func Load() (*Config, error) {
	path := filepath.Join(os.Getenv("HOME"), ".config", "aih", "config.toml")
	var c Config
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &c); err != nil {
			return nil, err
		}
	}
	return &c, nil
}
