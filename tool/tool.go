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

func (r *Registry) Register(t interface{}) error {
	var tool Tool
	switch v := t.(type) {
	case Tool:
		tool = v
	case *Builder:
		built, err := v.Build()
		if err != nil {
			return err
		}
		tool = built
	default:
		return errors.New("tool must implement Tool interface or be *Builder")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if name == "" {
		return errors.New("tool name cannot be empty")
	}

	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}

	r.tools[name] = tool
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

type Builder struct {
	name        string
	description string
	params      map[string]interface{}
	required    []string
	action      func(ctx context.Context, params map[string]any) (any, error)
}

func New(name string) *Builder {
	return &Builder{
		name:   name,
		params: make(map[string]interface{}),
	}
}

func (t *Builder) Desc(desc string) *Builder {
	t.description = desc
	return t
}

func (t *Builder) Param(name, typ, desc string, required bool) *Builder {
	t.params[name] = map[string]interface{}{
		"type":        typ,
		"description": desc,
	}
	if required {
		t.required = append(t.required, name)
	}
	return t
}

func (t *Builder) ParamWithSchema(name string, schema map[string]interface{}, required bool) *Builder {
	t.params[name] = schema
	if required {
		t.required = append(t.required, name)
	}
	return t
}

func (t *Builder) Action(fn func(ctx context.Context, params map[string]any) (any, error)) *Builder {
	t.action = fn
	return t
}

func (t *Builder) Build() (Tool, error) {
	if t.name == "" {
		return nil, errors.New("tool name is required")
	}
	if t.action == nil {
		return nil, errors.New("tool action is required")
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": t.params,
	}
	if len(t.required) > 0 {
		schema["required"] = t.required
	}

	return &funcTool{
		name:        t.name,
		description: t.description,
		parameters:  schema,
		execute:     t.action,
	}, nil
}

func (t *Builder) MustBuild() Tool {
	tool, err := t.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build tool: %v", err))
	}
	return tool
}

func (r *Registry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(fmt.Sprintf("failed to register tool: %v", err))
	}
}

func (r *Registry) MustRegisterFunc(name, description string, params map[string]interface{}, fn func(ctx context.Context, params map[string]any) (any, error)) {
	if err := r.RegisterFunc(name, description, params, fn); err != nil {
		panic(fmt.Sprintf("failed to register tool function: %v", err))
	}
}

// Backward compatibility
type FluentTool = Builder

// RegisterQuick registers a simple tool with auto-generated schema
func (r *Registry) RegisterQuick(name string, fn func(ctx context.Context, params map[string]any) (any, error)) error {
	return r.Register(&funcTool{
		name:        name,
		description: "Auto-registered tool: " + name,
		parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		execute: fn,
	})
}
