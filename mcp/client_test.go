package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ratrektlabs/rakit/mcp"
)

// fakeServer mounts a tiny JSON-RPC MCP HTTP endpoint.
type fakeServer struct {
	initCalls      int
	listToolsCalls int
	callToolCalls  int
}

func (f *fakeServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		reply := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  result,
			})
		}

		switch req.Method {
		case "initialize":
			f.initCalls++
			reply(map[string]any{
				"serverInfo": map[string]any{"name": "fake", "version": "1"},
			})
		case "tools/list":
			f.listToolsCalls++
			reply(map[string]any{
				"tools": []map[string]any{
					{
						"name":        "echo",
						"description": "echo input",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			})
		case "tools/call":
			f.callToolCalls++
			reply(map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "hello"},
				},
			})
		default:
			http.Error(w, "unknown method "+req.Method, http.StatusBadRequest)
		}
	}
}

func TestClientDiscoverAndCall(t *testing.T) {
	f := &fakeServer{}
	ts := httptest.NewServer(f.handler())
	defer ts.Close()

	c := mcp.NewClientForTransport(ts.URL, nil, "http")

	tools, err := c.Discover(context.Background(), "fake")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools len=%d", len(tools))
	}
	if tools[0].ToolName != "echo" || tools[0].ServerName != "fake" {
		t.Fatalf("tool=%+v", tools[0])
	}

	result, err := c.CallTool(context.Background(), "echo", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result == "" {
		t.Fatal("CallTool returned empty result")
	}

	if f.initCalls == 0 {
		t.Fatal("initialize not called")
	}
	if f.listToolsCalls == 0 {
		t.Fatal("tools/list not called")
	}
	if f.callToolCalls == 0 {
		t.Fatal("tools/call not called")
	}
}

func TestClientReportsServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID int `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error": map[string]any{
				"code":    -32603,
				"message": "internal server error",
			},
		})
	}))
	defer ts.Close()

	c := mcp.NewClientForTransport(ts.URL, nil, "http")
	_, err := c.Discover(context.Background(), "fake")
	if err == nil {
		t.Fatal("expected error from server")
	}
}
