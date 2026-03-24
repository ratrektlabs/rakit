package tool

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrMissingName        = errors.New("tool name is required")
	ErrMissingDescription = errors.New("tool description is required")
	ErrMissingAction      = errors.New("tool action is required")
	ErrEmptyParameterName = errors.New("parameter name cannot be empty")
)

type Builder struct {
	name        string
	description string
	parameters  map[string]ParameterSchema
	action      ExecuteFunc
	err         error
}

func New(name string) *Builder {
	return &Builder{
		name:       name,
		parameters: make(map[string]ParameterSchema),
	}
}

func (b *Builder) Description(desc string) *Builder {
	if b.err != nil {
		return b
	}
	b.description = desc
	return b
}

func (b *Builder) Param(name string, typ ParameterType, desc string, required bool) *Builder {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = ErrEmptyParameterName
		return b
	}
	b.parameters[name] = ParameterSchema{
		Type:        typ,
		Description: desc,
		Required:    required,
	}
	return b
}

func (b *Builder) ParamWithDefault(name string, typ ParameterType, desc string, required bool, def any) *Builder {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = ErrEmptyParameterName
		return b
	}
	b.parameters[name] = ParameterSchema{
		Type:        typ,
		Description: desc,
		Required:    required,
		Default:     def,
	}
	return b
}

func (b *Builder) ParamWithEnum(name string, typ ParameterType, desc string, required bool, enum []any) *Builder {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = ErrEmptyParameterName
		return b
	}
	b.parameters[name] = ParameterSchema{
		Type:        typ,
		Description: desc,
		Required:    required,
		Enum:        enum,
	}
	return b
}

func (b *Builder) ParamWithItems(name string, typ ParameterType, desc string, required bool, items *ParameterSchema) *Builder {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = ErrEmptyParameterName
		return b
	}
	b.parameters[name] = ParameterSchema{
		Type:        typ,
		Description: desc,
		Required:    required,
		Items:       items,
	}
	return b
}

func (b *Builder) ParamWithSchema(name string, schema ParameterSchema) *Builder {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = ErrEmptyParameterName
		return b
	}
	b.parameters[name] = schema
	return b
}

func (b *Builder) Action(fn ExecuteFunc) *Builder {
	if b.err != nil {
		return b
	}
	b.action = fn
	return b
}

func (b *Builder) ActionFunc(fn func(ctx context.Context, args map[string]any) (any, error)) *Builder {
	return b.Action(fn)
}

func (b *Builder) Build() (Tool, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.name == "" {
		return nil, ErrMissingName
	}
	if b.description == "" {
		return nil, ErrMissingDescription
	}
	if b.action == nil {
		return nil, ErrMissingAction
	}
	return &FuncTool{
		name:        b.name,
		description: b.description,
		parameters:  b.parameters,
		execute:     b.action,
	}, nil
}

func (b *Builder) MustBuild() Tool {
	tool, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("tool builder: %v", err))
	}
	return tool
}
