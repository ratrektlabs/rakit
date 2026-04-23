package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/ratrektlabs/rakit/tool"
)

// HTTPTool is a tool that calls an HTTP endpoint.
type HTTPTool struct {
	name          string
	description   string
	parameters    any
	endpoint      string
	headers       map[string]string
	inputMap      map[string]string
	responseField string
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

		// Extract a specific field if responseField is set
		if t.responseField != "" {
			if m, ok := result.(map[string]any); ok {
				if v, ok := m[t.responseField]; ok {
					result = v
				}
			}
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

		// Determine the interpreter from the file extension
		ext := filepath.Ext(t.scriptPath)
		var cmd *exec.Cmd
		switch ext {
		case ".sh", ".bash":
			cmd = exec.CommandContext(ctx, "bash", "-c", string(data))
		case ".py":
			cmd = exec.CommandContext(ctx, "python3", "-c", string(data))
		case ".js", ".mjs":
			cmd = exec.CommandContext(ctx, "node", "--input-type=module", "-")
			cmd.Stdin = bytes.NewReader(data)
		default:
			// Try bash as fallback
			cmd = exec.CommandContext(ctx, "bash", "-c", string(data))
		}

		// Pass tool input as JSON via stdin (if not already set for JS)
		if ext != ".js" && ext != ".mjs" {
			inputJSON, _ := json.Marshal(input)
			cmd.Stdin = bytes.NewReader(inputJSON)
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return tool.Err(
				fmt.Sprintf("script error: %v\n%s", err, string(output)),
				fmt.Sprintf("Check the script at %q for errors", t.scriptPath),
			), nil
		}

		return tool.Ok(string(output)), nil
	})
}

// NewHTTPTool creates an HTTPTool from its constituent parts.
func NewHTTPTool(name, description string, parameters any, endpoint string, headers map[string]string, inputMap map[string]string, responseField string) *HTTPTool {
	return &HTTPTool{
		name:          name,
		description:   description,
		parameters:    parameters,
		endpoint:      endpoint,
		headers:       headers,
		inputMap:      inputMap,
		responseField: responseField,
	}
}

// NewScriptTool creates a ScriptTool from its constituent parts.
func NewScriptTool(name, description string, parameters any, scriptPath string, rm *ResourceManager) *ScriptTool {
	return &ScriptTool{
		name:        name,
		description: description,
		parameters:  parameters,
		scriptPath:  scriptPath,
		resources:   rm,
	}
}

// ToolFromDef builds a tool.Tool from a skill ToolDef.
func ToolFromDef(def ToolDef, rm *ResourceManager) (tool.Tool, error) {
	switch def.Handler {
	case "http", "":
		if def.Endpoint == "" {
			return nil, fmt.Errorf("tool %q: http handler requires an endpoint", def.Name)
		}
		return NewHTTPTool(def.Name, def.Description, def.Parameters, def.Endpoint, def.Headers, def.InputMapping, def.ResponseField), nil
	case "script":
		if def.ScriptPath == "" {
			return nil, fmt.Errorf("tool %q: script handler requires a script_path", def.Name)
		}
		return NewScriptTool(def.Name, def.Description, def.Parameters, def.ScriptPath, rm), nil
	default:
		return nil, fmt.Errorf("tool %q: unknown handler %q", def.Name, def.Handler)
	}
}
