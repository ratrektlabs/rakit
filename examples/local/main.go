package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ratrektlabs/rakit/agent"
	"github.com/ratrektlabs/rakit/protocol"
	"github.com/ratrektlabs/rakit/protocol/agui"
	"github.com/ratrektlabs/rakit/protocol/aisdk"
	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/provider/gemini"
	"github.com/ratrektlabs/rakit/provider/openai"
	"github.com/ratrektlabs/rakit/skill"
	blobLocal "github.com/ratrektlabs/rakit/storage/blob/local"
	"github.com/ratrektlabs/rakit/storage/metadata"
	metaSQLite "github.com/ratrektlabs/rakit/storage/metadata/sqlite"
	"github.com/ratrektlabs/rakit/tool"
)

//go:embed index.html
var frontendFS embed.FS

// providerConfig is the persisted provider configuration stored in the metadata store.
type providerConfig struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
}

// envOr returns the value of the named env var, or def if unset/empty.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// loadProviderConfig tries to create a provider from env vars, falling back to
// config persisted in the metadata store. Returns nil if no config is available.
//
// Model names can be overridden with OPENAI_MODEL / GEMINI_MODEL env vars so
// the example keeps working as providers rotate their model lineup.
func loadProviderConfig(ctx context.Context, store metadata.Store) (provider.Provider, error) {
	// Try environment variables first
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		return gemini.New(envOr("GEMINI_MODEL", "gemini-2.5-flash"), geminiKey)
	}
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey != "" {
		return openai.New(envOr("OPENAI_MODEL", "gpt-4o-mini"), openaiKey), nil
	}

	// Fall back to persisted config
	data, err := store.Get(ctx, "__config:provider")
	if err != nil || data == nil {
		return nil, nil // No saved config — that's okay
	}

	var cfg providerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid saved provider config: %w", err)
	}
	if cfg.APIKey == "" {
		return nil, nil
	}

	switch cfg.Provider {
	case "gemini":
		return gemini.New(cfg.Model, cfg.APIKey)
	case "openai":
		return openai.New(cfg.Model, cfg.APIKey), nil
	default:
		return nil, fmt.Errorf("unknown saved provider: %s", cfg.Provider)
	}
}

func main() {
	ctx := context.Background()

	// Storage (local — no external services required)
	store, err := metaSQLite.NewStore(ctx, "./data/agent.db")
	if err != nil {
		log.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	blobStore, err := blobLocal.New("./data/workspace")
	if err != nil {
		log.Fatalf("Failed to create local blob store: %v", err)
	}

	// Provider — try env vars first, then fall back to saved config in store
	var prov provider.Provider
	if p, err := loadProviderConfig(ctx, store); err != nil {
		log.Printf("Warning: could not load saved provider config: %v", err)
	} else if p != nil {
		prov = p
	}

	// Protocol registry
	reg := protocol.NewRegistry()
	reg.Register(aisdk.New())
	reg.Register(agui.New())
	reg.SetDefault(aisdk.New())

	// Agent
	//
	// `delete_item` is gated by an [agent.ApprovalPolicy] so the LLM cannot
	// invoke it without explicit human approval — the runner raises an
	// AG-UI interrupt and pauses the run until the FE sends a resume[].
	a := agent.New(
		agent.WithProvider(prov),
		agent.WithProtocol(aisdk.New()),
		agent.WithStore(store),
		agent.WithFS(blobStore),
		agent.WithApprovalPolicy(agent.RequireFor("delete_item")),
	)

	// Register the spawn_agent tool so the LLM can delegate subtasks
	a.Tools.Register(a.SpawnAgentTool(reg.Default()))

	// browser_time is a client-side tool: the runner emits the tool call
	// and then pauses on a tool_call interrupt. The browser supplies the
	// result via /chat resume[]. This is the spec-native AI SDK
	// "tool with no execute" pattern wrapped behind the [agent.ClientSide]
	// marker interface.
	a.Tools.Register(&clientSideTool{
		name:        "browser_time",
		description: "Return the user's wall-clock time and timezone (executes in the browser).",
		parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	})

	// Register demo skills.
	_ = a.Skills.Register(ctx, &skill.Definition{
		Name:         "echo",
		Description:  "Echoes back the input",
		Instructions: "Use the echo tool to repeat what the user says.",
		Tools: []skill.ToolDef{{
			Name:        "echo",
			Description: "Echo back the input text",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
			Handler:       "http",
			Endpoint:      "https://httpbin.org/post",
			ResponseField: "json",
		}},
	})

	// delete_item exists so the approval-gated HIL flow has something
	// concrete to demonstrate. It hits httpbin so the flow is visible
	// end-to-end without any external state.
	_ = a.Skills.Register(ctx, &skill.Definition{
		Name:         "danger_zone",
		Description:  "Destructive operations that require human approval before running.",
		Instructions: "Use delete_item when the user asks to delete something. The framework will pause for approval before the tool runs.",
		Tools: []skill.ToolDef{{
			Name:        "delete_item",
			Description: "Permanently delete an item by id (requires human approval).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
			Handler:       "http",
			Endpoint:      "https://httpbin.org/post",
			ResponseField: "json",
		}},
	})

	// HTTP handler
	mux := http.NewServeMux()

	mux.HandleFunc("POST /chat", func(w http.ResponseWriter, r *http.Request) {
		if a.Provider == nil {
			http.Error(w, "no provider configured — set one via Provider settings", http.StatusServiceUnavailable)
			return
		}

		p := reg.Negotiate(r.Header.Get("Accept"))
		if p == nil {
			p = reg.Default()
		}

		w.Header().Set("Content-Type", p.ContentType())

		// The single /chat envelope carries either a fresh user turn
		// (Message) or a resume of one or more open interrupts
		// (Resume). This matches AG-UI contract rule 2 — there is no
		// rakit-specific /chat/resume.
		var req struct {
			Message   string         `json:"message"`
			SessionID string         `json:"sessionId"`
			UserID    string         `json:"userId"`
			Resume    []resumeInWire `json:"resume"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Default user
		if req.UserID == "" {
			req.UserID = "default"
		}

		isResume := len(req.Resume) > 0

		// Create session if not provided. A resume turn must reference
		// an existing session — there are no open interrupts on a new
		// session.
		if req.SessionID == "" {
			if isResume {
				http.Error(w, "resume requires sessionId", http.StatusBadRequest)
				return
			}
			sess, err := a.CreateSessionForUser(r.Context(), req.UserID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.SessionID = sess.ID
		}

		var (
			events <-chan agent.Event
			runErr error
		)
		if isResume {
			// Both AG-UI and AI SDK callers route resumes through this
			// envelope. AG-UI carries interruptId per spec; the AI SDK
			// has no interrupt concept on the wire, so its callers
			// supply toolCallId — we resolve it to the open interrupt
			// here so [Agent.Resume] gets a uniform list.
			inputs, resolveErr := resolveResumeInputs(r.Context(), a, req.SessionID, req.Resume)
			if resolveErr != nil {
				http.Error(w, resolveErr.Error(), http.StatusBadRequest)
				return
			}
			events, runErr = a.Resume(r.Context(), req.SessionID, inputs, p)
		} else {
			events, runErr = a.RunWithSession(r.Context(), req.SessionID, req.Message, p)
		}
		if runErr != nil {
			http.Error(w, runErr.Error(), http.StatusInternalServerError)
			return
		}

		if err := p.EncodeStream(r.Context(), w, events); err != nil {
			if r.Context().Err() == nil {
				log.Printf("Stream error: %v", err)
			}
		}
	})

	// Admin API
	registerAdminHandlers(mux, a)

	// Frontend — serve embedded index.html at /
	dist, _ := fs.Sub(frontendFS, ".")
	mux.Handle("GET /", http.FileServer(http.FS(dist)))

	addr := ":8080"
	fmt.Printf("Agent server listening on %s\n", addr)
	fmt.Println("Data stored in ./data/")
	fmt.Println("Admin API at /api/v1/")
	if prov == nil {
		fmt.Println("No provider configured — set one via the dashboard Provider settings")
	}
	fmt.Printf("Dashboard at http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, corsMiddleware(requestLogger(mux))))
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

type loggingWriter struct {
	http.ResponseWriter
	status int
}

func (lw *loggingWriter) WriteHeader(code int) {
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

// resumeInWire is the JSON shape the FE sends to resolve open interrupts.
//
// It mirrors AG-UI's RunAgentInput.resume[] verbatim. AI SDK callers, which
// have no interrupt concept on the wire, supply [ToolCallID] instead — the
// server resolves it to the matching open [agent.Interrupt] before calling
// [agent.Agent.Resume].
type resumeInWire struct {
	InterruptID string `json:"interruptId"`
	ToolCallID  string `json:"toolCallId"`
	Status      string `json:"status"`
	Payload     any    `json:"payload"`
}

// resolveResumeInputs translates the wire-shape resume entries into the
// [agent.ResumeInput] slice the agent expects, mapping toolCallId to the
// matching open interruptId for AI SDK callers.
func resolveResumeInputs(ctx context.Context, a *agent.Agent, sessionID string, in []resumeInWire) ([]agent.ResumeInput, error) {
	sess, err := a.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if sess == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	byTool := make(map[string]string, len(sess.OpenInterrupts))
	for _, intr := range sess.OpenInterrupts {
		if intr.ToolCallID != "" {
			byTool[intr.ToolCallID] = intr.ID
		}
	}
	out := make([]agent.ResumeInput, len(in))
	for i, ri := range in {
		intrID := ri.InterruptID
		if intrID == "" && ri.ToolCallID != "" {
			intrID = byTool[ri.ToolCallID]
		}
		if intrID == "" {
			return nil, fmt.Errorf("resume entry %d missing interruptId/toolCallId", i)
		}
		status := agent.ResumeResolved
		if ri.Status == "cancelled" {
			status = agent.ResumeCancelled
		}
		out[i] = agent.ResumeInput{
			InterruptID: intrID,
			Status:      status,
			Payload:     ri.Payload,
		}
	}
	return out, nil
}

// clientSideTool is a tool the runner advertises to the LLM but never
// executes inline. The [ClientSide] marker tells the runner to raise an
// interrupt instead, so the FE can supply the result.
type clientSideTool struct {
	name        string
	description string
	parameters  map[string]any
}

func (t *clientSideTool) Name() string        { return t.name }
func (t *clientSideTool) Description() string { return t.description }
func (t *clientSideTool) Parameters() any     { return t.parameters }

// Execute is unreachable in normal use — the runner intercepts client-side
// tools before invoking Execute. We keep a defensive implementation so a
// misconfigured agent (no [ClientSide] marker honored) surfaces a clear
// error instead of silently returning empty output.
func (t *clientSideTool) Execute(ctx context.Context, input map[string]any) (*tool.Result, error) {
	return nil, fmt.Errorf("%s is a client-side tool and cannot be executed in-process", t.name)
}

// ClientSide marks this tool as client-executed.
func (t *clientSideTool) ClientSide() bool { return true }

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		h.Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
