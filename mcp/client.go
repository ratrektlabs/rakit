package mcp

import (
	"context"
	"fmt"
	"log"
)

// Client is an MCP client that communicates via a pluggable Transport.
type Client struct {
	transport Transport
}

// NewClient creates a client using an HTTP transport (default).
func NewClient(url string, headers map[string]string) *Client {
	return &Client{
		transport: NewHTTPTransport(url, headers),
	}
}

// NewClientWithTransport creates a client using a specific transport.
func NewClientWithTransport(t Transport) *Client {
	return &Client{transport: t}
}

// NewClientForTransport creates a client using the appropriate transport type.
func NewClientForTransport(url string, headers map[string]string, transportType string) *Client {
	var t Transport
	switch transportType {
	case "sse":
		t = NewSSETransport(url, headers)
	default:
		t = NewHTTPTransport(url, headers)
	}
	return &Client{transport: t}
}

// Initialize sends the MCP initialize request and returns server info.
func (c *Client) Initialize(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	err := c.transport.Send(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "rakit",
			"version": "0.1.0",
		},
	}, &result)
	if err != nil {
		return nil, err
	}

	// Send initialized notification
	_ = c.transport.Notify(ctx, "notifications/initialized", nil)

	return result, nil
}

// ListTools sends tools/list and returns the discovered tools.
func (c *Client) ListTools(ctx context.Context) ([]MCPToolDef, error) {
	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		} `json:"tools"`
	}

	if err := c.transport.Send(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, fmt.Errorf("mcp: list tools: %w", err)
	}

	var tools []MCPToolDef
	for _, t := range result.Tools {
		tools = append(tools, MCPToolDef{
			ToolName:    t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}
	return tools, nil
}

// CallTool sends tools/call for a specific tool and returns the result.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (any, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	if err := c.transport.Send(ctx, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	}, &result); err != nil {
		return nil, fmt.Errorf("mcp: call tool %q: %w", toolName, err)
	}

	if result.IsError {
		var errMsg string
		for _, c := range result.Content {
			errMsg += c.Text
		}
		return nil, fmt.Errorf("mcp: tool %q returned error: %s", toolName, errMsg)
	}

	// Return text content as-is; for single text content, return just the string
	if len(result.Content) == 1 && result.Content[0].Type == "text" {
		return result.Content[0].Text, nil
	}

	// Multiple content blocks — return as-is
	return result.Content, nil
}

// Ping sends a ping to verify the server is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.transport.Send(ctx, "ping", nil, nil)
}

// Discover connects to the MCP server, initializes, and lists available tools.
func (c *Client) Discover(ctx context.Context, serverName string) ([]MCPToolDef, error) {
	if _, err := c.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("mcp: initialize %q: %w", serverName, err)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp: discover %q: %w", serverName, err)
	}

	// Tag each tool with the server name
	for i := range tools {
		tools[i].ServerName = serverName
	}

	log.Printf("mcp: discovered %d tools from server %q", len(tools), serverName)
	return tools, nil
}

// Close releases transport resources.
func (c *Client) Close() error {
	return c.transport.Close()
}
