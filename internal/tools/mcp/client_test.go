package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// mockTransport implements Transport for testing.
type mockTransport struct {
	responses map[string]json.RawMessage
	errors    map[string]error
	calls     []mockCall
	closed    bool
}

type mockCall struct {
	Method string
	Params any
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		responses: make(map[string]json.RawMessage),
		errors:    make(map[string]error),
	}
}

func (m *mockTransport) SetResponse(method string, result any) {
	data, _ := json.Marshal(result)
	m.responses[method] = data
}

func (m *mockTransport) SetError(method string, err error) {
	m.errors[method] = err
}

func (m *mockTransport) Send(_ context.Context, method string, params any) (json.RawMessage, error) {
	m.calls = append(m.calls, mockCall{Method: method, Params: params})

	if err, ok := m.errors[method]; ok {
		return nil, err
	}
	if resp, ok := m.responses[method]; ok {
		return resp, nil
	}
	// Default empty result for unknown methods (e.g., notifications).
	return json.RawMessage(`{}`), nil
}

func (m *mockTransport) Notify(_ context.Context, method string, params any) error {
	m.calls = append(m.calls, mockCall{Method: method, Params: params})
	return nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func TestMCPClientListTools(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
	})
	mock.SetResponse("tools/list", toolsListResult{
		Tools: []MCPToolDef{
			{
				Name:        "read_file",
				Description: "Read a file",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
			{
				Name:        "write_file",
				Description: "Write a file",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`),
			},
		},
	})

	client := NewMCPClient("test", mock)
	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("tool[0].Name = %q, want read_file", tools[0].Name)
	}
	if tools[1].Name != "write_file" {
		t.Errorf("tool[1].Name = %q, want write_file", tools[1].Name)
	}
}

func TestMCPClientCallTool(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("tools/call", toolCallResult{
		Content: []contentBlock{
			{Type: "text", Text: "file contents here"},
		},
	})

	client := NewMCPClient("test", mock)
	output, err := client.CallTool(context.Background(), "read_file", map[string]any{"path": "test.txt"})

	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if output != "file contents here" {
		t.Errorf("output = %q, want 'file contents here'", output)
	}
}

func TestMCPClientCallToolError(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("tools/call", toolCallResult{
		Content: []contentBlock{
			{Type: "text", Text: "file not found"},
		},
		IsError: true,
	})

	client := NewMCPClient("test", mock)
	_, err := client.CallTool(context.Background(), "read_file", map[string]any{"path": "missing.txt"})

	if err == nil {
		t.Error("expected error for isError response")
	}
}

func TestMCPClientCallToolTransportError(t *testing.T) {
	mock := newMockTransport()
	mock.SetError("tools/call", fmt.Errorf("connection refused"))

	client := NewMCPClient("test", mock)
	_, err := client.CallTool(context.Background(), "read_file", map[string]any{})

	if err == nil {
		t.Error("expected error for transport failure")
	}
}

func TestMCPClientClose(t *testing.T) {
	mock := newMockTransport()
	client := NewMCPClient("test", mock)

	if err := client.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !mock.closed {
		t.Error("expected transport to be closed")
	}
}

func TestMCPClientMultipleContentBlocks(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("tools/call", toolCallResult{
		Content: []contentBlock{
			{Type: "text", Text: "line 1"},
			{Type: "text", Text: "line 2"},
			{Type: "image", Text: "ignored"},
		},
	})

	client := NewMCPClient("test", mock)
	output, err := client.CallTool(context.Background(), "tool", map[string]any{})

	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if output != "line 1\nline 2" {
		t.Errorf("output = %q, want 'line 1\\nline 2'", output)
	}
}
