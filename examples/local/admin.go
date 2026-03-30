package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ratrektlabs/rakit/agent"
	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/provider/gemini"
	"github.com/ratrektlabs/rakit/provider/openai"
	"github.com/ratrektlabs/rakit/storage/metadata"
)

const providerConfigKey = "__config:provider"

// registerAdminHandlers registers admin API routes on the given mux.
// All routes are prefixed with /api/v1/.
func registerAdminHandlers(mux *http.ServeMux, a *agent.Agent) {
	h := &adminHandler{agent: a}

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
	mux.HandleFunc("PUT /api/v1/provider", h.setProvider)
}

type adminHandler struct {
	agent *agent.Agent
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

func (h *adminHandler) requireStore(w http.ResponseWriter) metadata.Store {
	if h.agent.Store == nil {
		writeError(w, http.StatusBadRequest, "no store configured")
		return nil
	}
	return h.agent.Store
}

// --- Sessions ---

func (h *adminHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	sessions, err := store.ListSessions(r.Context(), h.agent.ID)
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

func (h *adminHandler) createSession(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	sess, err := store.CreateSession(r.Context(), h.agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": sess})
}

func (h *adminHandler) getSession(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	id := r.PathValue("id")
	sess, err := store.GetSession(r.Context(), id)
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

func (h *adminHandler) deleteSession(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	id := r.PathValue("id")
	if err := store.DeleteSession(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Skills ---

func (h *adminHandler) listSkills(w http.ResponseWriter, r *http.Request) {
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

func (h *adminHandler) registerSkill(w http.ResponseWriter, r *http.Request) {
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

func (h *adminHandler) getSkill(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	def, err := store.GetSkill(r.Context(), name)
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

func (h *adminHandler) deleteSkill(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	if err := store.DeleteSkill(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *adminHandler) enableSkill(w http.ResponseWriter, r *http.Request) {
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

func (h *adminHandler) disableSkill(w http.ResponseWriter, r *http.Request) {
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

func (h *adminHandler) listTools(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	tools, err := store.ListTools(r.Context(), h.agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (h *adminHandler) saveTool(w http.ResponseWriter, r *http.Request) {
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

	if err := store.SaveTool(r.Context(), &td); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *adminHandler) getTool(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	td, err := store.GetTool(r.Context(), name)
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

func (h *adminHandler) deleteTool(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	name := r.PathValue("name")
	if err := store.DeleteTool(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Memory ---

func (h *adminHandler) getMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing key parameter")
		return
	}
	value, err := store.Get(r.Context(), key)
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

func (h *adminHandler) setMemory(w http.ResponseWriter, r *http.Request) {
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
	if err := store.Set(r.Context(), body.Key, []byte(body.Value)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *adminHandler) deleteMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	key := r.PathValue("key")
	if err := store.Delete(r.Context(), key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *adminHandler) listMemory(w http.ResponseWriter, r *http.Request) {
	store := h.requireStore(w)
	if store == nil {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	keys, err := store.List(r.Context(), prefix)
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

func (h *adminHandler) getProvider(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"configured": h.agent.Provider != nil,
	}

	if h.agent.Provider != nil {
		resp["provider"] = h.agent.Provider.Name()
		resp["model"] = h.agent.Provider.Model()
		resp["models"] = h.agent.Provider.Models()
	}

	// Return persisted config info (with masked API key)
	if store := h.agent.Store; store != nil {
		if data, err := store.Get(r.Context(), providerConfigKey); err == nil && data != nil {
			var cfg map[string]string
			if json.Unmarshal(data, &cfg) == nil {
				saved := map[string]any{
					"provider": cfg["provider"],
					"model":    cfg["model"],
					"hasKey":   cfg["apiKey"] != "",
				}
				if key := cfg["apiKey"]; key != "" {
					if len(key) > 8 {
						saved["apiKeyMasked"] = key[:4] + "..." + key[len(key)-4:]
					} else {
						saved["apiKeyMasked"] = "****"
					}
				}
				resp["saved"] = saved
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *adminHandler) setModel(w http.ResponseWriter, r *http.Request) {
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

func (h *adminHandler) setProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if body.Provider == "" || body.APIKey == "" {
		writeError(w, http.StatusBadRequest, "missing provider or apiKey")
		return
	}
	if body.Model == "" {
		writeError(w, http.StatusBadRequest, "missing model")
		return
	}

	var p provider.Provider
	var err error
	switch body.Provider {
	case "gemini":
		p, err = gemini.New(body.Model, body.APIKey)
	case "openai":
		p = openai.New(body.Model, body.APIKey)
	default:
		writeError(w, http.StatusBadRequest, "unknown provider: "+body.Provider)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create provider: %v", err))
		return
	}

	// Persist config to store so it survives restarts
	if store := h.agent.Store; store != nil {
		cfgData, _ := json.Marshal(map[string]string{
			"provider": body.Provider,
			"apiKey":   body.APIKey,
			"model":    body.Model,
		})
		if err := store.Set(r.Context(), providerConfigKey, cfgData); err != nil {
			// Log but don't fail — the provider is still active in-memory
			fmt.Printf("Warning: failed to persist provider config: %v\n", err)
		}
	}

	h.agent.Provider = p
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"provider": p.Name(),
		"model":    p.Model(),
		"models":   p.Models(),
	})
}
