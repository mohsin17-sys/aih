package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The test binary re-execs itself as a fake MCP server (stdio transport).
func TestMain(m *testing.M) {
	if os.Getenv("FRIDAY_FAKE_MCP") == "1" {
		fakeMCPServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeMCPServer() {
	var mu sync.Mutex // serializes writes to stdout
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	reply := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
		out.Flush()
	}
	if pid := os.Getenv("FAKE_MCP_PIDFILE"); pid != "" {
		os.WriteFile(pid, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			if os.Getenv("FAKE_MCP_FAIL_INIT") == "1" {
				reply(map[string]any{"jsonrpc": "2.0", "id": req.ID,
					"error": map[string]any{"code": -32000, "message": "boom"}})
				continue
			}
			reply(map[string]any{"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{"protocolVersion": "2024-11-05"}})
		case "tools/list":
			reply(map[string]any{"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo the text argument",
					"inputSchema": map[string]any{"type": "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
						"required":   []string{"text"}},
				}}}})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			// respond out of order and slowly to stress response routing
			time.Sleep(time.Duration(req.ID%7) * 10 * time.Millisecond)
			reply(map[string]any{"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{"content": []map[string]any{
					{"type": "text", "text": p.Arguments["text"]}}}})
		}
	}
}

func newFakeServer(t *testing.T, extraEnv ...string) (*Server, string) {
	t.Helper()
	pidfile := t.TempDir() + "/pid"
	env := append([]string{
		"FRIDAY_FAKE_MCP=1",
		"FAKE_MCP_PIDFILE=" + pidfile,
	}, extraEnv...)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		Name: "fake", Command: exe, Env: env, Trusted: true,
	}, pidfile
}

func serverPID(t *testing.T, pidfile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidfile); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err == nil {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fake server never wrote its pidfile")
	return 0
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func TestConnectCallAndClose(t *testing.T) {
	s, pidfile := newFakeServer(t)
	if err := s.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if tools := s.Tools(); len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
	out, err := s.Call("echo", map[string]any{"text": "hello"})
	if err != nil || out != "hello" {
		t.Fatalf("call = %q, %v", out, err)
	}

	pid := serverPID(t, pidfile)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatal("Close did not terminate the server process")
	}
	// Close is idempotent; calls after Close fail cleanly
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := s.Call("echo", nil); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("call after close = %v, want not-connected error", err)
	}
}

func TestConcurrentCallsRouteResponses(t *testing.T) {
	s, _ := newFakeServer(t)
	if err := s.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("payload-%d", i)
			got, err := s.Call("echo", map[string]any{"text": want})
			if err != nil {
				errs <- fmt.Errorf("call %d: %w", i, err)
				return
			}
			if got != want {
				errs <- fmt.Errorf("call %d got %q, want %q (response cross-stealing)", i, got, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestConnectFailureTerminatesProcess(t *testing.T) {
	s, pidfile := newFakeServer(t, "FAKE_MCP_FAIL_INIT=1")
	if err := s.Connect(); err == nil {
		t.Fatal("connect should fail")
		s.Close()
	}
	pid := serverPID(t, pidfile)
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatal("failed connect leaked the server process")
	}
}
