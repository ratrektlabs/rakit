package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ratrektlabs/rakit/tool"
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

func (t *HTTPTool) Execute(ctx context.Context, input map[string]any) (*tool.Result, error) {
	return tool.Measure(func() (*tool.Result, error) {
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
			return tool.Err(
				fmt.Sprintf("create request: %v", err),
				"Check that the endpoint URL is valid",
			), nil
		}

		req.Header.Set("Content-Type", "application/json")
		for k, v := range t.headers {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return tool.Err(
				fmt.Sprintf("request failed: %v", err),
				"Check network connectivity and endpoint availability",
			), nil
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return tool.Err(
				fmt.Sprintf("read response: %v", err),
				"The endpoint returned an unreadable response",
			), nil
		}

		if resp.StatusCode >= 400 {
			return tool.Err(
				fmt.Sprintf("%s: %s", resp.Status, respBody),
				fmt.Sprintf("Verify the endpoint %q is working and accepts the expected input", t.endpoint),
			), nil
		}

		var result any
		if err := json.Unmarshal(respBody, &result); err != nil {
			return tool.Ok(string(respBody)), nil
		}
		return tool.Ok(result), nil
	})
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

func (t *ScriptTool) Execute(ctx context.Context, input map[string]any) (*tool.Result, error) {
	return tool.Measure(func() (*tool.Result, error) {
		if t.resources == nil {
			return tool.Err(
				"no resource manager configured",
				"Provide a ResourceManager when creating the ScriptTool",
			), nil
		}

		data, err := t.resources.Load(ctx, t.scriptPath)
		if err != nil {
			return tool.Err(
				fmt.Sprintf("load script: %v", err),
				fmt.Sprintf("Ensure the script exists at path %q in the blob store", t.scriptPath),
			), nil
		}

		// Script execution is a placeholder — actual implementation depends on
		// the runtime environment (sandboxed exec, WASM, etc.)
		return tool.Ok(string(data)), nil
	})
}
