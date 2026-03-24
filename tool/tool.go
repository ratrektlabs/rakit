package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ratrektlabs/rl-agent/provider"
)

var (
	ErrToolNotFound     = errors.New("tool not found")
	ErrToolExists       = errors.New("tool already exists")
	ErrInvalidSchema    = errors.New("invalid tool schema")
	ErrExecutionFailed  = errors.New("tool execution failed")
	ErrValidationFailed = errors.New("validation failed")
	ErrMissingRequired  = errors.New("missing required parameter")
	ErrInvalidType      = errors.New("invalid parameter type")
	ErrInvalidEnumValue = errors.New("invalid enum value")
)

type ValidationError struct {
	Parameter string
	Message   string
	Err       error
}

func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("parameter %q: %s: %v", e.Parameter, e.Message, e.Err)
	}
	return fmt.Sprintf("parameter %q: %s", e.Parameter, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

type ParameterType string

const (
	TypeString  ParameterType = "string"
	TypeNumber  ParameterType = "number"
	TypeInteger ParameterType = "integer"
	TypeBoolean ParameterType = "boolean"
	TypeArray   ParameterType = "array"
	TypeObject  ParameterType = "object"
)

type ParameterSchema struct {
	Type        ParameterType              `json:"type"`
	Description string                     `json:"description,omitempty"`
	Required    bool                       `json:"-"`
	Enum        []any                      `json:"enum,omitempty"`
	Default     any                        `json:"default,omitempty"`
	Items       *ParameterSchema           `json:"items,omitempty"`
	Properties  map[string]ParameterSchema `json:"properties,omitempty"`
}

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]ParameterSchema
	Execute(ctx context.Context, args map[string]any) (any, error)
}

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]ParameterSchema
}

type ToolRegistry interface {
	Register(tool Tool) error
	Get(name string) (Tool, error)
	List() []ToolInfo
	ToProviderTools() []provider.ToolDefinition
}

type ExecuteFunc func(ctx context.Context, args map[string]any) (any, error)

type FuncTool struct {
	name        string
	description string
	parameters  map[string]ParameterSchema
	execute     ExecuteFunc
}

func (t *FuncTool) Name() string                           { return t.name }
func (t *FuncTool) Description() string                    { return t.description }
func (t *FuncTool) Parameters() map[string]ParameterSchema { return t.parameters }
func (t *FuncTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.execute(ctx, args)
}

type registry struct {
	tools sync.Map
}

func NewRegistry() ToolRegistry {
	return &registry{}
}

func (r *registry) Register(tool Tool) error {
	if tool == nil {
		return ErrInvalidSchema
	}
	name := tool.Name()
	if name == "" {
		return ErrInvalidSchema
	}
	_, loaded := r.tools.LoadOrStore(name, tool)
	if loaded {
		return fmt.Errorf("%w: %s", ErrToolExists, name)
	}
	return nil
}

func (r *registry) Get(name string) (Tool, error) {
	val, ok := r.tools.Load(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return val.(Tool), nil
}

func (r *registry) List() []ToolInfo {
	var list []ToolInfo
	r.tools.Range(func(key, value any) bool {
		tool := value.(Tool)
		list = append(list, ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
		return true
	})
	return list
}

func (r *registry) ToProviderTools() []provider.ToolDefinition {
	var tools []provider.ToolDefinition
	r.tools.Range(func(key, value any) bool {
		tool := value.(Tool)
		tools = append(tools, provider.ToolDefinition{
			Type: "function",
			Function: provider.ToolFunctionDef{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  schemaToMap(tool.Parameters()),
			},
		})
		return true
	})
	return tools
}

func schemaToMap(params map[string]ParameterSchema) map[string]any {
	props := make(map[string]any)
	var required []string
	for name, p := range params {
		props[name] = paramToMap(p)
		if p.Required {
			required = append(required, name)
		}
	}
	result := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

func paramToMap(p ParameterSchema) map[string]any {
	m := map[string]any{"type": string(p.Type)}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if len(p.Enum) > 0 {
		m["enum"] = p.Enum
	}
	if p.Default != nil {
		m["default"] = p.Default
	}
	if p.Items != nil {
		m["items"] = paramToMap(*p.Items)
	}
	if len(p.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range p.Properties {
			props[k] = paramToMap(v)
		}
		m["properties"] = props
	}
	return m
}

func Validate(tool Tool, args map[string]any) error {
	if tool == nil {
		return ErrInvalidSchema
	}
	params := tool.Parameters()
	var errs []error
	for name, schema := range params {
		val, exists := args[name]
		if !exists {
			if schema.Required {
				errs = append(errs, &ValidationError{
					Parameter: name,
					Message:   "required parameter missing",
					Err:       ErrMissingRequired,
				})
			}
			continue
		}
		if err := validateType(name, schema, val); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 1 {
		return errs[0]
	}
	if len(errs) > 1 {
		return fmt.Errorf("%w: %v", ErrValidationFailed, errs)
	}
	return nil
}

func validateType(name string, schema ParameterSchema, val any) error {
	if val == nil {
		if schema.Required {
			return &ValidationError{
				Parameter: name,
				Message:   "value is nil",
				Err:       ErrMissingRequired,
			}
		}
		return nil
	}
	switch schema.Type {
	case TypeString:
		if _, ok := val.(string); !ok {
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("expected string, got %T", val),
				Err:       ErrInvalidType,
			}
		}
	case TypeNumber:
		switch val.(type) {
		case float64, float32, int, int64, int32:
		default:
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("expected number, got %T", val),
				Err:       ErrInvalidType,
			}
		}
	case TypeInteger:
		switch val.(type) {
		case int, int64, int32:
		default:
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("expected integer, got %T", val),
				Err:       ErrInvalidType,
			}
		}
	case TypeBoolean:
		if _, ok := val.(bool); !ok {
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("expected boolean, got %T", val),
				Err:       ErrInvalidType,
			}
		}
	case TypeArray:
		if _, ok := val.([]any); !ok {
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("expected array, got %T", val),
				Err:       ErrInvalidType,
			}
		}
	case TypeObject:
		if _, ok := val.(map[string]any); !ok {
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("expected object, got %T", val),
				Err:       ErrInvalidType,
			}
		}
	}
	if len(schema.Enum) > 0 {
		found := false
		for _, e := range schema.Enum {
			if e == val {
				found = true
				break
			}
		}
		if !found {
			return &ValidationError{
				Parameter: name,
				Message:   fmt.Sprintf("value %v not in enum %v", val, schema.Enum),
				Err:       ErrInvalidType,
			}
		}
	}
	return nil
}

func ExecuteWithValidation(ctx context.Context, tool Tool, args map[string]any) (any, error) {
	if err := Validate(tool, args); err != nil {
		return nil, err
	}
	return tool.Execute(ctx, args)
}
