package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

func TestMcpToolToMatterTool(t *testing.T) {
	mock := newMockTransport()
	client := NewMCPClient("github", mock)

	mcpTool := MCPToolDef{
		Name:        "create_issue",
		Description: "Create a GitHub issue",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
	}

	tool := mcpToolToMatterTool("github", mcpTool, client, 0)

	if tool.Name != "github.create_issue" {
		t.Errorf("name = %q, want 'github.create_issue'", tool.Name)
	}
	if tool.Description != "Create a GitHub issue" {
		t.Errorf("description = %q, want 'Create a GitHub issue'", tool.Description)
	}
	if tool.Safe {
		t.Error("MCP tools should be unsafe by default")
	}
	if !tool.SideEffect {
		t.Error("MCP tools should have side effects by default")
	}
	if tool.Timeout != DefaultToolTimeout {
		t.Errorf("timeout = %v, want %v", tool.Timeout, DefaultToolTimeout)
	}
	if tool.Execute == nil {
		t.Error("Execute function should be set")
	}
}

func TestMcpToolToMatterToolCustomTimeout(t *testing.T) {
	mock := newMockTransport()
	client := NewMCPClient("db", mock)
	mcpTool := MCPToolDef{Name: "query", Description: "Run SQL"}

	tool := mcpToolToMatterTool("db", mcpTool, client, 60*time.Second)

	if tool.Timeout != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", tool.Timeout)
	}
}

func TestMcpToolNamespacing(t *testing.T) {
	mock := newMockTransport()
	client := NewMCPClient("server1", mock)

	tools := []MCPToolDef{
		{Name: "read", Description: "read"},
		{Name: "write", Description: "write"},
	}

	names := make(map[string]bool)
	for _, td := range tools {
		tool := mcpToolToMatterTool("server1", td, client, 0)
		names[tool.Name] = true
	}

	if !names["server1.read"] {
		t.Error("expected server1.read")
	}
	if !names["server1.write"] {
		t.Error("expected server1.write")
	}
}

func TestMcpExecuteFuncSuccess(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("tools/call", toolCallResult{
		Content: []contentBlock{{Type: "text", Text: "result data"}},
	})

	client := NewMCPClient("test", mock)
	fn := mcpExecuteFunc(client, "my_tool")

	result, err := fn(context.Background(), map[string]any{"key": "val"})

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Output != "result data" {
		t.Errorf("output = %q, want 'result data'", result.Output)
	}
	if result.Error != "" {
		t.Errorf("unexpected tool error: %s", result.Error)
	}
}

func TestMcpExecuteFuncError(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("tools/call", toolCallResult{
		Content: []contentBlock{{Type: "text", Text: "not found"}},
		IsError: true,
	})

	client := NewMCPClient("test", mock)
	fn := mcpExecuteFunc(client, "my_tool")

	result, err := fn(context.Background(), map[string]any{})

	// MCP errors are returned as recoverable ToolResult.Error, not Go errors.
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected tool error in result")
	}
}

func TestDiscoverAndRegister(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	})
	mock.SetResponse("tools/list", toolsListResult{
		Tools: []MCPToolDef{
			{Name: "tool_a", Description: "Tool A", InputSchema: json.RawMessage(`{}`)},
			{Name: "tool_b", Description: "Tool B", InputSchema: json.RawMessage(`{}`)},
		},
	})

	var registered []matter.Tool
	registerFn := func(tool matter.Tool) error {
		registered = append(registered, tool)
		return nil
	}

	client, err := DiscoverAndRegister(context.Background(), "myserver", mock, 0, registerFn)
	if err != nil {
		t.Fatalf("DiscoverAndRegister failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	if len(registered) != 2 {
		t.Fatalf("registered %d tools, want 2", len(registered))
	}
	if registered[0].Name != "myserver.tool_a" {
		t.Errorf("tool[0].Name = %q, want 'myserver.tool_a'", registered[0].Name)
	}
	if registered[1].Name != "myserver.tool_b" {
		t.Errorf("tool[1].Name = %q, want 'myserver.tool_b'", registered[1].Name)
	}
}

func TestDiscoverAndRegisterInitFails(t *testing.T) {
	mock := newMockTransport()
	mock.SetError("initialize", context.DeadlineExceeded)

	_, err := DiscoverAndRegister(context.Background(), "bad", mock, 0, func(matter.Tool) error { return nil })
	if err == nil {
		t.Error("expected error when initialization fails")
	}
	if !mock.closed {
		t.Error("transport should be closed on failure")
	}
}

func TestDiscoverAndRegisterListFails(t *testing.T) {
	mock := newMockTransport()
	mock.SetResponse("initialize", map[string]any{})
	mock.SetError("tools/list", context.DeadlineExceeded)

	_, err := DiscoverAndRegister(context.Background(), "bad", mock, 0, func(matter.Tool) error { return nil })
	if err == nil {
		t.Error("expected error when tools/list fails")
	}
	if !mock.closed {
		t.Error("transport should be closed on failure")
	}
}
