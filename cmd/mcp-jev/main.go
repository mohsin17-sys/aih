package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os"

    "aiharness"
)

type rpcRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params"`
}

type callParams struct {
    Name      string `json:"name"`
    Arguments struct {
        Query string   `json:"query"`
        Tools []string `json:"tools"`
    } `json:"arguments"`
}

func writeMsg(v any) {
    out, _ := json.Marshal(v)
    fmt.Println(string(out))
}

func reply(id json.RawMessage, result any) {
    writeMsg(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func replyErr(id json.RawMessage, msg string) {
    writeMsg(map[string]any{"jsonrpc": "2.0", "id": id,
        "error": map[string]any{"code": -32000, "message": msg}})
}

var toolDef = map[string]any{
    "name":        "jev_query",
    "description": "Ask the Jev orchestrator which MCP tool (filesystem, sqlite, docker, github, puppeteer, mempalace) should handle a task. Returns the selected tool, confidence score, and probability distribution.",
    "inputSchema": map[string]any{
        "type": "object",
        "properties": map[string]any{
            "query": map[string]any{"type": "string",
                "description": "Natural-language description of the task/state"},
            "tools": map[string]any{"type": "array",
                "items":       map[string]any{"type": "string"},
                "description": "Candidate MCP tools, e.g. [\"filesystem\",\"docker\"]"},
        },
        "required": []string{"query", "tools"},
    },
}

func main() {
    scanner := bufio.NewScanner(os.Stdin)
    scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
    for scanner.Scan() {
        var req rpcRequest
        if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
            continue
        }
        switch req.Method {
        case "initialize":
            reply(req.ID, map[string]any{
                "protocolVersion": "2024-11-05",
                "capabilities":    map[string]any{"tools": map[string]any{}},
                "serverInfo":      map[string]any{"name": "mcp-jev", "version": "1.0.0"},
            })
        case "notifications/initialized":
            // no response for notifications
        case "ping":
            if req.ID != nil {
                reply(req.ID, map[string]any{})
            }
        case "tools/list":
            reply(req.ID, map[string]any{"tools": []any{toolDef}})
        case "tools/call":
            var p callParams
            json.Unmarshal(req.Params, &p)
            resp, status, err := aiharness.QueryJevDecided(p.Arguments.Query, p.Arguments.Tools)
            if err != nil {
                replyErr(req.ID, err.Error())
                continue
            }
            if status < 200 || status >= 300 {
                replyErr(req.ID, fmt.Sprintf("jev returned HTTP %d", status))
                continue
            }
            ans := resp.Answers["selected_tool"]
            text := fmt.Sprintf("Jev (%s) selected tool: %s (confidence %.2f). Probabilities: %v. Tokens: %d in / %d out.",
    resp.Model, ans.Choice, ans.Confidence, ans.Probabilities,
    resp.Usage.InputTokens, resp.Usage.OutputTokens)
		reply(req.ID, map[string]any{
                "content": []any{map[string]any{"type": "text", "text": text}},
            })
        }
    }
}
