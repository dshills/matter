package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/dshills/matter/pkg/matter"
)

// DefaultToolTimeout is the default timeout for MCP tool execution.
const DefaultToolTimeout = 30 * time.Second

// DiscoveryTimeout is the timeout for MCP tool discovery (tools/list).
const DiscoveryTimeout = 10 * time.Second

// mcpToolToMatterTool converts an MCP tool definition to a matter.Tool.
// MCP tools are registered as unsafe with side effects by default.
// Tool names are namespaced as "servername.toolname" to avoid collisions.
func mcpToolToMatterTool(serverName string, mcpTool MCPToolDef, client *MCPClient, timeout time.Duration) matter.Tool {
	if timeout <= 0 {
		timeout = DefaultToolTimeout
	}
	return matter.Tool{
		Name:        serverName + "." + mcpTool.Name,
		Description: mcpTool.Description,
		InputSchema: mcpTool.InputSchema,
		Timeout:     timeout,
		Safe:        false,
		SideEffect:  true,
		Execute:     mcpExecuteFunc(client, mcpTool.Name),
	}
}

// mcpExecuteFunc returns a ToolExecuteFunc that calls the MCP server.
func mcpExecuteFunc(client *MCPClient, toolName string) matter.ToolExecuteFunc {
	return func(ctx context.Context, input map[string]any) (matter.ToolResult, error) {
		output, err := client.CallTool(ctx, toolName, input)
		if err != nil {
			// Return as a recoverable tool error (not a Go error that would
			// be treated as fatal). The planner can see the error and replan.
			return matter.ToolResult{Error: fmt.Sprintf("MCP call failed: %s", err)}, nil
		}
		return matter.ToolResult{Output: output}, nil
	}
}

// DiscoverAndRegister connects to an MCP server, discovers its tools, and
// registers them in the provided registry. Returns the client for lifecycle
// management (caller must Close it when done).
//
// Returns an error if the server cannot be reached or tool discovery fails.
// The caller decides whether this error is fatal (it typically isn't).
func DiscoverAndRegister(ctx context.Context, serverName string, transport Transport, timeout time.Duration, register func(matter.Tool) error) (*MCPClient, error) {
	client := NewMCPClient(serverName, transport)

	// Initialize the MCP handshake.
	initCtx, cancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer cancel()

	if err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("MCP server %q initialization failed: %w", serverName, err)
	}

	// Discover tools.
	listCtx, listCancel := context.WithTimeout(ctx, DiscoveryTimeout)
	defer listCancel()

	tools, err := client.ListTools(listCtx)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("MCP server %q tool discovery failed: %w", serverName, err)
	}

	// Register each tool.
	for _, mcpTool := range tools {
		tool := mcpToolToMatterTool(serverName, mcpTool, client, timeout)
		if err := register(tool); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("failed to register MCP tool %q: %w", tool.Name, err)
		}
	}

	return client, nil
}
