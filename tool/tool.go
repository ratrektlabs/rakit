package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ratrektlabs/rl-agent/provider"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, params map[string]any) (any, error)
}

type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	if name == "" {
		return errors.New("tool name cannot be empty")
	}

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}

	r.tools[name] = t
	return nil
}

func (r *Registry) RegisterFunc(name, description string, params map[string]interface{}, fn func(ctx context.Context, params map[string]any) (any, error)) error {
	return r.Register(&funcTool{
		name:        name,
		description: description,
		parameters:  params,
		execute:     fn,
	})
}

func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool %q not found", name)
	}

	delete(r.tools, name)
	return nil
}

func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return t, nil
}

func (r *Registry) List() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ToolInfo, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return result
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0, len(r.tools))
	for name := range r.tools {
		result = append(result, name)
	}
	return result
}

func (r *Registry) ToProviderTools() []provider.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]provider.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, provider.ToolDefinition{
			Type: "function",
			Function: provider.ToolFunctionDefinition{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return result
}

func (r *Registry) Execute(ctx context.Context, name string, params map[string]any) (any, error) {
	t, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	return t.Execute(ctx, params)
}

func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = make(map[string]Tool)
}

type funcTool struct {
	name        string
	description string
	parameters  map[string]interface{}
	execute     func(ctx context.Context, params map[string]any) (any, error)
}

func (t *funcTool) Name() string {
	return t.name
}

func (t *funcTool) Description() string {
	return t.description
}

func (t *funcTool) Parameters() map[string]interface{} {
	return t.parameters
}

func (t *funcTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	if t.execute == nil {
		return nil, errors.New("execute function is nil")
	}
	return t.execute(ctx, params)
}

type BaseTool struct {
	name        string
	description string
	parameters  map[string]interface{}
}

func (t *BaseTool) Name() string {
	return t.name
}

func (t *BaseTool) Description() string {
	return t.description
}

func (t *BaseTool) Parameters() map[string]interface{} {
	return t.parameters
}

func StringParam(description string, required bool) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
		"required":    required,
	}
}

func NumberParam(description string, required bool) map[string]interface{} {
	return map[string]interface{}{
		"type":        "number",
		"description": description,
		"required":    required,
	}
}

func BooleanParam(description string, required bool) map[string]interface{} {
	return map[string]interface{}{
		"type":        "boolean",
		"description": description,
		"required":    required,
	}
}

func ObjectParam(description string, properties map[string]interface{}, required []string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"properties":  properties,
		"required":    required,
	}
}

func ArrayParam(description string, items map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

func Schema(properties map[string]interface{}, required []string) map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func ParamsFromJSON(schema json.RawMessage) (map[string]interface{}, error) {
	var params map[string]interface{}
	if err := json.Unmarshal(schema, &params); err != nil {
		return nil, fmt.Errorf("failed to parse JSON schema: %w", err)
	}
	return params, nil
}
