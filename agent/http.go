package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

// RegisterHandlers registers admin API routes on the given mux.
// All routes are prefixed with /api/v1/.
func RegisterHandlers(mux *http.ServeMux, a *Agent) {
	h := &handler{agent: a}

	// Sessions
	mux.HandleFunc("GET /api/v1/sessions", h.listSessions)
	mux.HandleFunc("POST /api/v1/sessions", h.createSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.getSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", h.deleteSession)

	// Skills
	mux.HandleFunc("GET /api/v1/skills", h.listSkills)
	mux.HandleFunc("POST /api/v1/skills", h.registerSkill)
	mux.HandleFunc("GET /api/v1/skills/{name}", h.getSkill)
	mux.HandleFunc("DELETE /api/v1/skills/{name}", h.deleteSkill)
	mux.HandleFunc("POST /api/v1/skills/{name}/enable", h.enableSkill)
	mux.HandleFunc("POST /api/v1/skills/{name}/disable", h.disableSkill)

	// Tools
	mux.HandleFunc("GET /api/v1/tools", h.listTools)
	mux.HandleFunc("POST /api/v1/tools", h.saveTool)
	mux.HandleFunc("GET /api/v1/tools/{name}", h.getTool)
	mux.HandleFunc("DELETE /api/v1/tools/{name}", h.deleteTool)

	// Memory
	mux.HandleFunc("GET /api/v1/memory", h.getMemory)
	mux.HandleFunc("POST /api/v1/memory", h.setMemory)
	mux.HandleFunc("DELETE /api/v1/memory/{key}", h.deleteMemory)
	mux.HandleFunc("GET /api/v1/memory/list", h.listMemory)

	// Provider
	mux.HandleFunc("GET /api/v1/provider", h.getProvider)
	mux.HandleFunc("PUT /api/v1/provider/model", h.setModel)
}

type handler struct {
	agent *Agent
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *handler) requireStore(w http.ResponseWriter) *metadata.Store {
	if h.agent.Store == nil {
		writeError(w, http.StatusBadRequest, "no store configured")
		return nil
	}
	return &h.agent.Store
}

// --- Sessions ---

func (h *handler) listSessions(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	sessions, err := (*store).ListSessions(r.Context(), h.agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type sessionSummary struct {
		ID           string `json:"id"`
		AgentID      string `json:"agentId"`
		MessageCount int    `json:"messageCount"`
		CreatedAt    int64  `json:"createdAt"`
		UpdatedAt    int64  `json:"updatedAt"`
	}

	out := make([]sessionSummary, len(sessions))
	for i, s := range sessions {
		out[i] = sessionSummary{
			ID:           s.ID,
			AgentID:      s.AgentID,
			MessageCount: len(s.Messages),
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (h *handler) createSession(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	sess, err := (*store).CreateSession(r.Context(), h.agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": sess})
}

func (h *handler) getSession(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	id := r.PathValue("id")
	sess, err := (*store).GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess})
}

func (h *handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	id := r.PathValue("id")
	if err := (*store).DeleteSession(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Skills ---

func (h *handler) listSkills(w http.ResponseWriter, r *http.Request) {
	if h.agent.Skills == nil {
		writeJSON(w, http.StatusOK, map[string]any{"skills": []any{}})
		return
	}
	entries, err := h.agent.Skills.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": entries})
}

func (h *handler) registerSkill(w http.ResponseWriter, r *http.Request) {
	if h.agent.Skills == nil {
		writeError(w, http.StatusBadRequest, "no skill registry configured")
		return
	}

	var def struct {
		Name         string         `json:"name"`
		Description  string         `json:"description"`
		Version      string         `json:"version"`
		Instructions string         `json:"instructions"`
		Tools        []any          `json:"tools"`
		Config       map[string]any `json:"config"`
		Resources    []any          `json:"resources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	skillDef := &metadata.SkillDef{
		Name:         def.Name,
		Description:  def.Description,
		Version:      def.Version,
		Instructions: def.Instructions,
		Tools:        def.Tools,
		Config:       def.Config,
		Resources:    def.Resources,
		Enabled:      true,
	}
	if err := h.agent.Store.SaveSkill(r.Context(), skillDef); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *handler) getSkill(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	def, err := (*store).GetSkill(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if def == nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": def})
}

func (h *handler) deleteSkill(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	if err := (*store).DeleteSkill(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) enableSkill(w http.ResponseWriter, r *http.Request) {
	if h.agent.Skills == nil {
		writeError(w, http.StatusBadRequest, "no skill registry configured")
		return
	}
	name := r.PathValue("name")
	if err := h.agent.Skills.Enable(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) disableSkill(w http.ResponseWriter, r *http.Request) {
	if h.agent.Skills == nil {
		writeError(w, http.StatusBadRequest, "no skill registry configured")
		return
	}
	name := r.PathValue("name")
	if err := h.agent.Skills.Disable(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Tools ---

func (h *handler) listTools(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	tools, err := (*store).ListTools(r.Context(), h.agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (h *handler) saveTool(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}

	var td metadata.ToolDef
	if err := json.NewDecoder(r.Body).Decode(&td); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	td.AgentID = h.agent.ID
	if td.Handler == "" {
		td.Handler = "http"
	}
	if td.CreatedAt == 0 {
		td.CreatedAt = time.Now().UnixMilli()
	}

	if err := (*store).SaveTool(r.Context(), &td); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *handler) getTool(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	td, err := (*store).GetTool(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if td == nil {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tool": td})
}

func (h *handler) deleteTool(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	if err := (*store).DeleteTool(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Memory ---

func (h *handler) getMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing key parameter")
		return
	}
	value, err := (*store).Get(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if value == nil {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": string(value)})
}

func (h *handler) setMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	if err := (*store).Set(r.Context(), body.Key, []byte(body.Value)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) deleteMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	key := r.PathValue("key")
	if err := (*store).Delete(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) listMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	if prefix == "" {
		prefix = ""
	}
	keys, err := (*store).List(r.Context(), prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// --- Provider ---

func (h *handler) getProvider(w http.ResponseWriter, r *http.Request) {
	if h.agent.Provider == nil {
		writeError(w, http.StatusBadRequest, "no provider configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider": h.agent.Provider.Name(),
		"model":    h.agent.Provider.Model(),
		"models":   h.agent.Provider.Models(),
	})
}

func (h *handler) setModel(w http.ResponseWriter, r *http.Request) {
	if h.agent.Provider == nil {
		writeError(w, http.StatusBadRequest, "no provider configured")
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if body.Model == "" {
		writeError(w, http.StatusBadRequest, "missing model")
		return
	}
	h.agent.Provider.SetModel(body.Model)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": body.Model})
}

// strconv helper (unused but kept for future use).
var _ = strconv.Atoi
