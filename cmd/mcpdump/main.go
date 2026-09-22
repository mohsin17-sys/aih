// mcpdump: connect to configured MCP servers, print each tool's raw
// inputSchema — for diagnosing Mistral schema rejections.
package main

import (
    "encoding/json"
    "fmt"
    "os"

    "aiharness/config"
	"aiharness/internal/agent"
    "aiharness/internal/mcp"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        fmt.Println("config:", err)
        os.Exit(1)
    }
    for name, s := range cfg.MCP.Servers {
        if s.Command == "" {
            continue
        }
        srv := &mcp.Server{Name: name, Command: s.Command, Args: s.Args, Env: s.Env, Trusted: s.Trusted}
        if err := srv.Connect(); err != nil {
            fmt.Printf("== %s: DISABLED: %v\n\n", name, err)
            continue
        }
        fmt.Printf("== %s: connected, %d tools\n", name, len(srv.Tools()))
        for _, t := range srv.Tools() {
                                    b, _ := json.MarshalIndent(agent.SanitizeSchema(t.InputSchema), "", "  ")
            if len(b) > 800 {
                b = append(b[:800:800], []byte("\n  ...TRUNCATED")...)
            }
            fmt.Printf("-- tool %s\n%s\n", t.Name, string(b))
        }
        fmt.Println()
    }
}
