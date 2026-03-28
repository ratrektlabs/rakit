package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPTool is a tool that calls an HTTP endpoint.
type HTTPTool struct {
	name        string
	description string
	parameters  any
	endpoint    string
	headers     map[string]string
	inputMap    map[string]string
}

func (t *HTTPTool) Name() string        { return t.name }
func (t *HTTPTool) Description() string { return t.description }
func (t *HTTPTool) Parameters() any     { return t.parameters }

func (t *HTTPTool) Execute(ctx context.Context, input map[string]any) (any, error) {
	var body io.Reader

	if len(t.inputMap) > 0 {
		mapped := make(map[string]any)
		for src, dst := range t.inputMap {
			if v, ok := input[src]; ok {
				mapped[dst] = v
			}
		}
		b, err := json.Marshal(mapped)
		if err != nil {
			return nil, fmt.Errorf("http tool %q: marshal body: %w", t.name, err)
		}
		body = bytes.NewReader(b)
	} else {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("http tool %q: marshal body: %w", t.name, err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("http tool %q: create request: %w", t.name, err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http tool %q: execute: %w", t.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http tool %q: read response: %w", t.name, err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http tool %q: %s: %s", t.name, resp.Status, respBody)
	}

	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	return result, nil
}

// ScriptTool is a tool that loads and executes a script from blob store.
type ScriptTool struct {
	name        string
	description string
	parameters  any
	scriptPath  string
	resources   *ResourceManager
}

func (t *ScriptTool) Name() string        { return t.name }
func (t *ScriptTool) Description() string { return t.description }
func (t *ScriptTool) Parameters() any     { return t.parameters }

func (t *ScriptTool) Execute(ctx context.Context, input map[string]any) (any, error) {
	if t.resources == nil {
		return nil, fmt.Errorf("script tool %q: no resource manager", t.name)
	}

	_, err := t.resources.Load(ctx, t.scriptPath)
	if err != nil {
		return nil, fmt.Errorf("script tool %q: load script: %w", t.name, err)
	}

	// Script execution is a placeholder — actual implementation depends on
	// the runtime environment (sandboxed exec, WASM, etc.)
	return map[string]any{
		"status":       "loaded",
		"script_path":  t.scriptPath,
		"input":        input,
		"message":      "script loaded but execution not yet implemented",
	}, nil
}
