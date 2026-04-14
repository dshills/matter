package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// SSETransport communicates with an MCP server via HTTP Server-Sent Events.
// It connects to an SSE endpoint to receive responses and sends requests
// via HTTP POST.
type SSETransport struct {
	baseURL    string
	postURL    atomic.Value // string; populated from SSE endpoint event
	httpClient *http.Client
	nextID     atomic.Int64
	closed     atomic.Bool

	// SSE response handling.
	mu         sync.Mutex
	pending    map[int64]chan sseResult
	sseReady   chan struct{} // closed when postURL is available or readSSE exits
	sseErr     error         // set if SSE stream ends before endpoint event
	closeReady sync.Once     // ensures sseReady is closed exactly once
	cancel     context.CancelFunc
}

type sseResult struct {
	data json.RawMessage
	err  error
}

// NewSSETransport connects to an MCP server's SSE endpoint and returns a transport.
func NewSSETransport(ctx context.Context, url string) (*SSETransport, error) {
	sseCtx, cancel := context.WithCancel(ctx)

	t := &SSETransport{
		baseURL: url,
		// No global Timeout on the HTTP client: the SSE GET connection is
		// long-lived (event stream), so a client-level timeout would kill it.
		// Individual POST requests inherit the caller's context deadline.
		httpClient: &http.Client{},
		pending:    make(map[int64]chan sseResult),
		sseReady:   make(chan struct{}),
		cancel:     cancel,
	}

	// Connect to SSE endpoint.
	req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to MCP SSE server at %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("MCP SSE server returned status %d", resp.StatusCode)
	}

	// Read SSE events in background.
	go t.readSSE(resp.Body)

	// Wait for the endpoint event that tells us where to POST.
	select {
	case <-t.sseReady:
		if t.sseErr != nil {
			cancel()
			return nil, t.sseErr
		}
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}

	return t, nil
}

// readSSE processes the SSE event stream.
func (t *SSETransport) readSSE(body io.ReadCloser) {
	defer func() { _ = body.Close() }()

	// Ensure sseReady is always closed when readSSE exits, even if the
	// endpoint event was never received. This prevents NewSSETransport
	// from hanging indefinitely.
	defer func() {
		t.closeReady.Do(func() {
			if t.sseErr == nil {
				t.sseErr = fmt.Errorf("SSE stream ended before endpoint event")
			}
			close(t.sseReady)
		})
	}()

	scanner := newLargeScanner(body)
	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = end of event.
			if eventType != "" && len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				t.handleSSEEvent(eventType, data)
			}
			eventType = ""
			dataLines = nil
			continue
		}

		if after, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			dataLines = append(dataLines, after)
		}
	}

	// Capture scanner errors (e.g., connection reset, buffer overflow)
	// and propagate them to sseErr so NewSSETransport and pending
	// requests see the real cause.
	scanErr := scanner.Err()
	if scanErr != nil {
		t.sseErr = scanErr
	}

	// Connection closed — notify all pending requests.
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, ch := range t.pending {
		closeErr := fmt.Errorf("SSE connection closed")
		if scanErr != nil {
			closeErr = fmt.Errorf("SSE connection error: %w", scanErr)
		}
		ch <- sseResult{err: closeErr}
		delete(t.pending, id)
	}
}

// handleSSEEvent processes a single SSE event.
func (t *SSETransport) handleSSEEvent(eventType, data string) {
	switch eventType {
	case "endpoint":
		// The server tells us where to POST requests.
		t.postURL.Store(resolveURL(t.baseURL, data))
		t.closeReady.Do(func() { close(t.sseReady) })

	case "message":
		// Parse JSON-RPC response and dispatch to waiting caller.
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			return
		}

		t.mu.Lock()
		ch, ok := t.pending[resp.ID]
		if ok {
			delete(t.pending, resp.ID)
		}
		t.mu.Unlock()

		if ok {
			if resp.Error != nil {
				ch <- sseResult{err: fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)}
			} else {
				ch <- sseResult{data: resp.Result}
			}
		}
	}
}

// Send sends a JSON-RPC 2.0 request via HTTP POST and waits for the response
// via the SSE stream.
func (t *SSETransport) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if t.closed.Load() {
		return nil, fmt.Errorf("transport closed")
	}

	// Check if the SSE stream has failed since initialization.
	t.mu.Lock()
	if t.sseErr != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("SSE stream failed: %w", t.sseErr)
	}
	t.mu.Unlock()

	id := t.nextID.Add(1)

	// Register pending response channel.
	ch := make(chan sseResult, 1)
	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// POST the request.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.postURL.Load().(string), bytes.NewReader(data))
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to create POST request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to POST to MCP server: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("MCP server POST returned status %d", resp.StatusCode)
	}

	// Wait for response via SSE.
	select {
	case result := <-ch:
		return result.data, result.err
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Notify sends a JSON-RPC 2.0 notification via HTTP POST (no response expected).
func (t *SSETransport) Notify(ctx context.Context, method string, params any) error {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.postURL.Load().(string), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create POST request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to POST notification: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// Close shuts down the SSE connection.
func (t *SSETransport) Close() error {
	if t.closed.Swap(true) {
		return nil
	}
	t.cancel()
	return nil
}

// resolveURL resolves a potentially relative URL against a base URL
// using the standard net/url package for correct handling of relative
// paths, query parameters, and edge cases.
func resolveURL(base, ref string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(refURL).String()
}
