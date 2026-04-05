package mcp

import (
	"context"
	"fmt"

	"github.com/ratrektlabs/rakit/tool"
)

// MCPTool adapts an MCP server tool into the tool.Tool interface.
type MCPTool struct {
	prefixedName string // "mcp/<server>/<tool>"
	description  string
	parameters   any
	client       *Client
	rawToolName  string // the actual name on the MCP server
	serverName   string
}

// NewMCPTool creates a tool.Tool backed by an MCP server.
func NewMCPTool(serverName, toolName, description string, parameters any, client *Client) *MCPTool {
	return &MCPTool{
		prefixedName: fmt.Sprintf("mcp/%s/%s", serverName, toolName),
		description:  description,
		parameters:   parameters,
		client:       client,
		rawToolName:  toolName,
		serverName:   serverName,
	}
}

func (t *MCPTool) Name() string        { return t.prefixedName }
func (t *MCPTool) Description() string { return t.description }
func (t *MCPTool) Parameters() any     { return t.parameters }

func (t *MCPTool) Execute(ctx context.Context, input map[string]any) (*tool.Result, error) {
	return tool.Measure(func() (*tool.Result, error) {
		result, err := t.client.CallTool(ctx, t.rawToolName, input)
		if err != nil {
			return tool.Err(
				fmt.Sprintf("MCP tool call failed: %v", err),
				"Check that the MCP server is running and the tool exists",
			), nil
		}
		return tool.Ok(result), nil
	})
}

// ServerName returns the MCP server name this tool belongs to.
func (t *MCPTool) ServerName() string { return t.serverName }

// RawName returns the tool name on the MCP server (without prefix).
func (t *MCPTool) RawName() string { return t.rawToolName }
