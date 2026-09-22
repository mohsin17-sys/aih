// Package mcp implements a minimal Model Context Protocol client over
// the stdio transport: initialize handshake, tool discovery, tool calls.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Server struct {
	Name    string
	Command string
	Args    []string
	Env     []string
	Trusted bool

	mu      sync.Mutex // guards pending, nextID, pipe, scanErr, closed
	pending map[int]chan rpcResponse
	writeMu sync.Mutex // serializes writes to pipe
	done    chan struct{}
	cmd     *exec.Cmd
	pipe    io.WriteCloser
	nextID  int
	tools   []ToolDef
	scanErr error
	closed  bool
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

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// Connect starts the server process and performs the MCP handshake.
// On failure the child process is terminated before returning, so a
// failing server never leaks a process.
func (s *Server) Connect() error {
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

	s.mu.Lock()
	s.nextID = 1
	s.pending = map[int]chan rpcResponse{}
	s.done = make(chan struct{})
	s.cmd = cmd
	s.pipe = stdinPipe
	s.mu.Unlock()

	go s.readLoop(stdoutPipe)

	// initialize
	var initRes struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := s.roundtrip("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "friday", "version": "1.0"},
	}, &initRes); err != nil {
		s.Close()
		return fmt.Errorf("mcp %s: initialize: %w", s.Name, err)
	}
	// initialized notification (no response expected)
	notif, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	s.writeMu.Lock()
	stdinPipe.Write(append(notif, '\n'))
	s.writeMu.Unlock()

	// discover tools
	var listRes struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := s.roundtrip("tools/list", map[string]any{}, &listRes); err != nil {
		s.Close()
		return fmt.Errorf("mcp %s: tools/list: %w", s.Name, err)
	}
	s.mu.Lock()
	s.tools = listRes.Tools
	s.mu.Unlock()
	return nil
}

// readLoop parses server output and routes each response to the pending
// call with the matching ID. Notifications and malformed lines are ignored.
// On exit it fails every in-flight call so no roundtrip blocks forever.
func (s *Server) readLoop(stdoutPipe io.Reader) {
	defer close(s.done)
	sc := bufio.NewScanner(stdoutPipe)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var resp rpcResponse
		if json.Unmarshal(sc.Bytes(), &resp) != nil || resp.ID == 0 {
			continue
		}
		s.mu.Lock()
		ch, ok := s.pending[resp.ID]
		delete(s.pending, resp.ID)
		s.mu.Unlock()
		if ok {
			ch <- resp // buffered; exactly one delivery per ID
		}
	}
	if err := sc.Err(); err != nil {
		s.mu.Lock()
		s.scanErr = err
		s.mu.Unlock()
	}
	s.failAll()
}

// failAll aborts every pending call (server closed or unreadable).
func (s *Server) failAll() {
	s.mu.Lock()
	pending := s.pending
	s.pending = map[int]chan rpcResponse{}
	s.mu.Unlock()
	for id, ch := range pending {
		select {
		case ch <- rpcResponse{ID: id, Error: &rpcError{Message: s.readErrorLocked()}}:
		default:
		}
	}
}

func (s *Server) readErrorLocked() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanErr != nil {
		return fmt.Sprintf("read error: %v", s.scanErr)
	}
	return "server closed"
}

// Close terminates the server process and closes its stdin pipe. It is
// safe to call more than once and on servers that never connected.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cmd, pipe := s.cmd, s.pipe
	s.pipe = nil
	s.mu.Unlock()

	var err error
	if pipe != nil {
		pipe.Close()
	}
	if cmd != nil && cmd.Process != nil {
		if kerr := cmd.Process.Kill(); kerr != nil && err == nil {
			err = kerr
		}
		cmd.Wait() // reap the child; exit status is irrelevant here
	}
	return err
}

// roundtrip sends a request and waits for the matching response. Each call
// registers its own response channel, so concurrent calls on one server
// cannot consume each other's responses, and writes to the child's stdin
// are serialized.
func (s *Server) roundtrip(method string, params any, out any) error {
	s.mu.Lock()
	if s.closed || s.pipe == nil {
		s.mu.Unlock()
		return fmt.Errorf("mcp %s: not connected", s.Name)
	}
	id := s.nextID
	s.nextID++
	pipe := s.pipe
	ch := make(chan rpcResponse, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	req, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	_, werr := pipe.Write(append(req, '\n'))
	s.writeMu.Unlock()
	if werr != nil {
		return fmt.Errorf("mcp %s: %s: %w", s.Name, method, werr)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("mcp %s: %s: %s", s.Name, method, resp.Error.Message)
		}
		return json.Unmarshal(resp.Result, out)
	case <-time.After(30 * time.Second):
		return fmt.Errorf("mcp %s: %s: timeout", s.Name, method)
	case <-s.done:
		return fmt.Errorf("mcp %s: %s: %s", s.Name, method, s.readErrorLocked())
	}
}

// Tools returns the discovered tool definitions.
func (s *Server) Tools() []ToolDef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools
}

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
