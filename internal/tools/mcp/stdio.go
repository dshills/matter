package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// safeEnvVars is the baseline environment inherited by MCP subprocesses.
var safeEnvVars = []string{"PATH", "HOME", "TMPDIR", "LANG"}

// maxScannerBuf is the maximum buffer size for reading JSON-RPC messages.
// MCP tool results (e.g., file reads, data exports) can be large, so we
// use 16 MB to avoid bufio.Scanner "token too long" errors with the
// default 64 KB limit.
const maxScannerBuf = 16 * 1024 * 1024

// newLargeScanner creates a bufio.Scanner with an increased buffer for
// reading large JSON-RPC messages from MCP servers.
func newLargeScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, maxScannerBuf), maxScannerBuf)
	return s
}

// stdioResult holds a dispatched response for a pending request.
type stdioResult struct {
	data json.RawMessage
	err  error
}

// StdioTransport communicates with an MCP server subprocess via stdin/stdout
// using newline-delimited JSON-RPC 2.0. A single background goroutine reads
// from stdout and dispatches responses to pending request channels by ID.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	nextID atomic.Int64
	closed atomic.Bool
	done   chan struct{} // closed when the process exits

	// Write serialization — only one request writes to stdin at a time.
	writeMu sync.Mutex

	// Pending response channels, keyed by JSON-RPC request ID.
	mu      sync.Mutex
	pending map[int64]chan stdioResult
	readErr error // set when the reader goroutine exits
}

// NewStdioTransport starts the MCP server subprocess and returns a transport.
// The subprocess inherits a restricted environment: PATH, HOME, TMPDIR, LANG
// plus any explicitly configured env vars.
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	// Build restricted environment.
	cmdEnv := make([]string, 0, len(safeEnvVars)+len(env))
	for _, key := range safeEnvVars {
		if val, ok := os.LookupEnv(key); ok {
			cmdEnv = append(cmdEnv, key+"="+val)
		}
	}
	for key, val := range env {
		// Expand environment variable references in values, restricted to
		// the safe baseline variables to avoid leaking sensitive host
		// credentials (e.g., GITHUB_TOKEN, AWS_SECRET_ACCESS_KEY) into
		// the subprocess.
		expanded := os.Expand(val, func(name string) string {
			for _, safe := range safeEnvVars {
				if name == safe {
					return os.Getenv(name)
				}
			}
			// Also allow references to other explicitly configured env vars.
			if v, ok := env[name]; ok {
				return v
			}
			return ""
		})
		cmdEnv = append(cmdEnv, key+"="+expanded)
	}
	cmd.Env = cmdEnv

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Redirect subprocess stderr to the application's stderr for diagnostics.
	// We use os.Stderr directly rather than the structured observer logger
	// because MCP server stderr is unstructured (stack traces, error messages)
	// and would not parse correctly as structured log entries.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start MCP server %q: %w", command, err)
	}

	t := &StdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[int64]chan stdioResult),
		done:    make(chan struct{}),
	}

	// Read stdout in a single background goroutine and dispatch responses
	// to pending request channels by ID.
	go t.readLoop(stdout)

	// Monitor process exit in background.
	go func() {
		_ = cmd.Wait()
		close(t.done)
	}()

	return t, nil
}

// readLoop reads newline-delimited JSON-RPC messages from stdout and
// dispatches responses to pending request channels. Runs in a single
// goroutine for the lifetime of the transport.
func (t *StdioTransport) readLoop(r io.Reader) {
	scanner := newLargeScanner(r)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			// Skip non-JSON lines (e.g., server debug output on stdout).
			continue
		}

		// Notifications have ID 0 (zero value) — skip them.
		if resp.ID == 0 {
			continue
		}

		t.mu.Lock()
		ch, ok := t.pending[resp.ID]
		if ok {
			delete(t.pending, resp.ID)
		}
		t.mu.Unlock()

		if !ok {
			// Response for an unknown/timed-out request — discard.
			continue
		}

		if resp.Error != nil {
			ch <- stdioResult{err: fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)}
		} else {
			ch <- stdioResult{data: resp.Result}
		}
	}

	// Reader exited — notify all pending requests.
	readErr := scanner.Err()
	if readErr == nil {
		readErr = fmt.Errorf("MCP server closed stdout")
	}

	t.mu.Lock()
	t.readErr = readErr
	for id, ch := range t.pending {
		ch <- stdioResult{err: fmt.Errorf("MCP server read failed: %w", readErr)}
		delete(t.pending, id)
	}
	t.mu.Unlock()
}

// Send sends a JSON-RPC 2.0 request and waits for the response.
func (t *StdioTransport) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if t.closed.Load() {
		return nil, fmt.Errorf("transport closed")
	}

	// Check if reader has failed.
	t.mu.Lock()
	if t.readErr != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("MCP server read failed: %w", t.readErr)
	}
	t.mu.Unlock()

	id := t.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Register pending response channel before writing.
	ch := make(chan stdioResult, 1)
	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()

	// Write request with newline delimiter.
	t.writeMu.Lock()
	_, writeErr := t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()

	if writeErr != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to write to MCP server: %w", writeErr)
	}

	// Wait for response from the read loop.
	select {
	case result := <-ch:
		return result.data, result.err
	case <-t.done:
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("MCP server process exited while waiting for response")
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Notify sends a JSON-RPC 2.0 notification (no ID, no response expected).
func (t *StdioTransport) Notify(_ context.Context, method string, params any) error {
	if t.closed.Load() {
		return fmt.Errorf("transport closed")
	}

	notif := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	_, err = t.stdin.Write(append(data, '\n'))
	return err
}

// Close terminates the MCP server subprocess. It closes stdin to signal
// the server to exit, then waits up to 5 seconds before sending SIGKILL.
func (t *StdioTransport) Close() error {
	if t.closed.Swap(true) {
		return nil // already closed
	}

	// Close stdin to signal the server to exit.
	_ = t.stdin.Close()

	// Wait for process to exit with timeout.
	select {
	case <-t.done:
		return nil
	case <-time.After(5 * time.Second):
	}

	// Process didn't exit — kill it.
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}

	// Wait for the kill to take effect.
	select {
	case <-t.done:
	case <-time.After(2 * time.Second):
	}

	return nil
}
