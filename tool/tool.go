package tool

import "context"

// Tool represents an executable capability the LLM can invoke.
type Tool interface {
	Name() string
	Description() string
	Parameters() any
	Execute(ctx context.Context, input map[string]any) (any, error)
}
