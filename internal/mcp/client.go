// Package mcp implements a minimal Model Context Protocol client over
// the stdio transport: initialize handshake, tool discovery, tool calls.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"os"
	"os/exec"
	"sync"
)

type Server struct {
	Name    string
	Command string
	Args    []string
	Env     []string
	Trusted bool

	mu     sync.Mutex
	done   chan struct{}
	cmd    *exec.Cmd
	pipe   io.WriteCloser
	stdin  chan []byte
	nextID int
	tools  []ToolDef
}

// ToolDef is one tool advertised by the server.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Connect starts the server process and performs the MCP handshake.
func (s *Server) Connect() error {
	s.mu.Lock()
	s.nextID = 1
	s.stdin = make(chan []byte, 64)
	s.done = make(chan struct{})
	s.mu.Unlock()

	cmd := exec.Command(s.Command, s.Args...)
	cmd.Env = append(os.Environ(), s.Env...)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp %s: start %s: %w", s.Name, s.Command, err)
	}
	s.cmd = cmd
	s.pipe = stdinPipe

	go func() {
		defer close(s.done)
		sc := bufio.NewScanner(stdoutPipe)
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			var resp rpcResponse
			if json.Unmarshal(sc.Bytes(), &resp) == nil && resp.ID != 0 {
				b, _ := json.Marshal(resp)
				s.stdin <- b
			}
		}
	}()

	// initialize
	var initRes struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := s.roundtrip("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "friday", "version": "1.0"},
	}, &initRes); err != nil {
		return fmt.Errorf("mcp %s: initialize: %w", s.Name, err)
	}
	// initialized notification (no response expected)
	notif, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	stdinPipe.Write(append(notif, '\n'))

	// discover tools
	var listRes struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := s.roundtrip("tools/list", map[string]any{}, &listRes); err != nil {
		return fmt.Errorf("mcp %s: tools/list: %w", s.Name, err)
	}
	s.tools = listRes.Tools
	return nil
}

// roundtrip sends a request and waits for the matching response.
func (s *Server) roundtrip(method string, params any, out any) error {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	pipe := s.pipe
	s.mu.Unlock()
	if pipe == nil {
		return fmt.Errorf("mcp %s: not connected", s.Name)
	}
	req, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if _, err := pipe.Write(append(req, '\n')); err != nil {
		return err
	}
	doneCh := s.doneCh()
	timeout := time.After(30 * time.Second)
	for {
		var raw []byte
		select {
			case raw = <-s.stdin:
			case <-timeout:
				return fmt.Errorf("mcp %s: %s: timeout", s.Name, method)
			case <-doneCh:
				return fmt.Errorf("mcp %s: server closed", s.Name)
		}
		var resp rpcResponse
		if json.Unmarshal(raw, &resp) != nil || resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp %s: %s: %s", s.Name, method, resp.Error.Message)
		}
		return json.Unmarshal(resp.Result, out)
	}
}

func (s *Server) doneCh() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// Tools returns the discovered tool definitions.
func (s *Server) Tools() []ToolDef { return s.tools }

// Call executes one tool and returns its text output.
func (s *Server) Call(name string, args map[string]any) (string, error) {
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	params := map[string]any{"name": name, "arguments": args}
	if err := s.roundtrip("tools/call", params, &res); err != nil {
		return "", err
	}
	out := ""
	for _, c := range res.Content {
		if c.Type == "text" {
			if out != "" {
				out += "\n"
			}
			out += c.Text
		}
	}
	if res.IsError {
		return out, fmt.Errorf("mcp %s: tool %s error: %s", s.Name, name, out)
	}
	return out, nil
}
