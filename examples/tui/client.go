package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Client wraps HTTP calls to the rakit agent server.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// --- Types ---

type SessionInfo struct {
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	MessageCount int    `json:"messageCount"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type ProviderInfo struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Models   []string `json:"models"`
}

type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
}

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Handler     string `json:"handler"`
	Endpoint    string `json:"endpoint"`
}

// --- Sessions ---

func (c *Client) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	resp, err := c.get(ctx, "/api/v1/sessions")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Sessions, nil
}

func (c *Client) CreateSession(ctx context.Context) (*SessionInfo, error) {
	resp, err := c.post(ctx, "/api/v1/sessions", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Session SessionInfo `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Session, nil
}

func (c *Client) DeleteSession(ctx context.Context, id string) error {
	resp, err := c.delete(ctx, "/api/v1/sessions/"+id)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// --- Skills ---

func (c *Client) ListSkills(ctx context.Context) ([]SkillInfo, error) {
	resp, err := c.get(ctx, "/api/v1/skills")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Skills []SkillInfo `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Skills, nil
}

func (c *Client) RegisterSkill(ctx context.Context, name, desc, instructions string) error {
	body := map[string]any{
		"name":         name,
		"description":  desc,
		"version":      "1.0.0",
		"instructions": instructions,
		"tools":        []any{},
		"config":       map[string]any{},
		"resources":    []any{},
	}
	_, err := c.post(ctx, "/api/v1/skills", body)
	return err
}

func (c *Client) DeleteSkill(ctx context.Context, name string) error {
	resp, err := c.delete(ctx, "/api/v1/skills/"+name)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) ToggleSkill(ctx context.Context, name string, enable bool) error {
	path := "/api/v1/skills/" + name + "/disable"
	if enable {
		path = "/api/v1/skills/" + name + "/enable"
	}
	resp, err := c.post(ctx, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// --- Tools ---

func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	resp, err := c.get(ctx, "/api/v1/tools")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *Client) SaveTool(ctx context.Context, name, desc, handler, endpoint string) error {
	body := map[string]any{
		"name":        name,
		"description": desc,
		"handler":     handler,
		"endpoint":    endpoint,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string"},
			},
		},
	}
	_, err := c.post(ctx, "/api/v1/tools", body)
	return err
}

func (c *Client) DeleteTool(ctx context.Context, name string) error {
	resp, err := c.delete(ctx, "/api/v1/tools/"+name)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// --- Provider ---

func (c *Client) GetProvider(ctx context.Context) (*ProviderInfo, error) {
	resp, err := c.get(ctx, "/api/v1/provider")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result ProviderInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) SetModel(ctx context.Context, model string) error {
	_, err := c.put(ctx, "/api/v1/provider/model", map[string]string{"model": model})
	return err
}

// --- Memory ---

func (c *Client) GetMemory(ctx context.Context, key string) (string, error) {
	resp, err := c.get(ctx, "/api/v1/memory?key="+key)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if v, ok := result["value"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (c *Client) SetMemory(ctx context.Context, key, value string) error {
	_, err := c.post(ctx, "/api/v1/memory", map[string]string{"key": key, "value": value})
	return err
}

func (c *Client) ListMemory(ctx context.Context, prefix string) ([]string, error) {
	resp, err := c.get(ctx, "/api/v1/memory/list?prefix="+prefix)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Keys, nil
}

// --- Chat (streaming) ---

func (c *Client) Chat(ctx context.Context, message, sessionID string) (*http.Response, error) {
	body := map[string]string{
		"message":   message,
		"sessionId": sessionID,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

// --- HTTP helpers ---

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

func (c *Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *Client) put(ctx context.Context, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.httpClient.Do(req)
}

func (c *Client) delete(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}
