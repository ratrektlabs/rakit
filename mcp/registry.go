package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"
	"github.com/ratrektlabs/rakit/tool"
)

// Registry manages MCP server configurations and tool discovery.
type Registry struct {
	store metadata.Store
}

// NewRegistry creates an MCP registry backed by a metadata store.
func NewRegistry(store metadata.Store) *Registry {
	return &Registry{store: store}
}

// List returns all configured MCP servers for an agent.
func (r *Registry) List(ctx context.Context, agentID string) ([]*metadata.MCPServerDef, error) {
	return r.store.ListMCPServers(ctx, agentID)
}

// Register saves an MCP server configuration.
func (r *Registry) Register(ctx context.Context, def *metadata.MCPServerDef) error {
	if def.ID == "" {
		def.ID = generateID()
	}
	if def.CreatedAt == 0 {
		def.CreatedAt = time.Now().UnixMilli()
	}
	return r.store.SaveMCPServer(ctx, def)
}

// Unregister removes an MCP server configuration.
func (r *Registry) Unregister(ctx context.Context, name string) error {
	return r.store.DeleteMCPServer(ctx, name)
}

// Enable enables an MCP server.
func (r *Registry) Enable(ctx context.Context, name string) error {
	s, err := r.store.GetMCPServer(ctx, name)
	if err != nil {
		return err
	}
	s.Enabled = true
	return r.store.SaveMCPServer(ctx, s)
}

// Disable disables an MCP server.
func (r *Registry) Disable(ctx context.Context, name string) error {
	s, err := r.store.GetMCPServer(ctx, name)
	if err != nil {
		return err
	}
	s.Enabled = false
	return r.store.SaveMCPServer(ctx, s)
}

// Get loads a single MCP server configuration.
func (r *Registry) Get(ctx context.Context, name string) (*metadata.MCPServerDef, error) {
	return r.store.GetMCPServer(ctx, name)
}

// DiscoverTools connects to all enabled MCP servers and returns their tools.
// Non-critical failures are logged as warnings.
func (r *Registry) DiscoverTools(ctx context.Context, agentID string) ([]tool.Tool, error) {
	servers, err := r.store.ListMCPServers(ctx, agentID)
	if err != nil {
		return nil, err
	}

	var allTools []tool.Tool
	for _, s := range servers {
		if !s.Enabled {
			continue
		}

		client := NewClientForTransport(s.URL, s.Headers, s.Transport)
		tools, err := client.Discover(ctx, s.Name)
		if err != nil {
			log.Printf("warning: mcp: failed to discover tools from %q: %v", s.Name, err)
			continue
		}

		for _, td := range tools {
			allTools = append(allTools, NewMCPTool(td.ServerName, td.ToolName, td.Description, td.Parameters, client))
		}
	}

	return allTools, nil
}

// DiscoverServer connects to a specific MCP server and returns its tools.
// This is useful for the admin UI to preview tools before saving.
func (r *Registry) DiscoverServer(ctx context.Context, def *metadata.MCPServerDef) ([]MCPToolDef, error) {
	client := NewClientForTransport(def.URL, def.Headers, def.Transport)
	return client.Discover(ctx, def.Name)
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
