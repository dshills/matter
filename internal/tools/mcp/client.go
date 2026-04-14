// Package mcp implements a client for the Model Context Protocol (MCP),
// enabling matter to discover and execute tools from external MCP servers
// via stdio or SSE transports.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Transport abstracts the communication channel to an MCP server.
type Transport interface {
	// Send sends a JSON-RPC 2.0 request and returns the result field from the response.
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	// Notify sends a JSON-RPC 2.0 notification (no ID, no response expected).
	Notify(ctx context.Context, method string, params any) error
	// Close shuts down the transport and releases resources.
	Close() error
}

// jsonRPCRequest is a JSON-RPC 2.0 request message.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

// jsonRPCNotification is a JSON-RPC 2.0 notification (no ID, no response expected).
type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response message.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

// jsonRPCError is a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPToolDef represents a tool definition from tools/list.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// toolsListResult is the result of a tools/list call.
type toolsListResult struct {
	Tools []MCPToolDef `json:"tools"`
}

// toolCallParams is the params for a tools/call request.
type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// toolCallResult is the result of a tools/call response.
type toolCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// contentBlock represents a content block in an MCP response.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPClient manages communication with a single MCP server.
type MCPClient struct {
	name      string
	transport Transport
}

// NewMCPClient creates a client for the named MCP server.
func NewMCPClient(name string, transport Transport) *MCPClient {
	return &MCPClient{
		name:      name,
		transport: transport,
	}
}

// Name returns the server name.
func (c *MCPClient) Name() string {
	return c.name
}

// Initialize sends the MCP initialize handshake.
func (c *MCPClient) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "matter",
			"version": "1.0.0",
		},
	}
	_, err := c.transport.Send(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("MCP initialize failed: %w", err)
	}

	// Send initialized notification (no ID, no response expected per MCP spec).
	_ = c.transport.Notify(ctx, "notifications/initialized", nil)
	return nil
}

// ListTools calls tools/list on the MCP server.
func (c *MCPClient) ListTools(ctx context.Context) ([]MCPToolDef, error) {
	raw, err := c.transport.Send(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	var result toolsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list response: %w", err)
	}

	return result.Tools, nil
}

// CallTool calls tools/call on the MCP server.
func (c *MCPClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (string, error) {
	params := toolCallParams{
		Name:      toolName,
		Arguments: arguments,
	}

	raw, err := c.transport.Send(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}

	var result toolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("failed to parse tools/call response: %w", err)
	}

	// Concatenate text content blocks.
	var output string
	for _, block := range result.Content {
		if block.Type == "text" {
			if output != "" {
				output += "\n"
			}
			output += block.Text
		}
	}

	if result.IsError {
		return "", fmt.Errorf("MCP tool error: %s", output)
	}

	return output, nil
}

// Close shuts down the transport.
func (c *MCPClient) Close() error {
	return c.transport.Close()
}
