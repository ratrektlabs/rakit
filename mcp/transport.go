package mcp

import "context"

// Transport is the interface for MCP communication transports.
type Transport interface {
	// Send sends a JSON-RPC request and decodes the response into result.
	Send(ctx context.Context, method string, params any, result any) error
	// Notify sends a JSON-RPC notification (no response expected).
	Notify(ctx context.Context, method string, params any) error
	// Close releases any resources held by the transport.
	Close() error
}
