package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Planner struct {
		Provider string `toml:"provider"`
		Model    string `toml:"model"`
	} `toml:"planner"`
	Editor struct {
		Provider string `toml:"provider"`
		BaseURL  string `toml:"base_url"`
		Model    string `toml:"model"`
	} `toml:"editor"`
	Jev struct {
		Enabled bool `toml:"enabled"`
	} `toml:"jev"`
	Safety struct {
		AutoApproveWrite bool    `toml:"auto_approve_write"`
		AutoApproveShell bool    `toml:"auto_approve_shell"`
		MaxCostUSD       float64 `toml:"max_cost_usd"`
	} `toml:"safety"`
	MCP struct {
		Servers map[string]struct {
			Command string `toml:"command"`
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
