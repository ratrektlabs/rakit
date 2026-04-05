package tool

import (
	"context"
	"time"
)

// Result wraps tool execution output with standard metadata.
type Result struct {
	Data       any    `json:"data"`       // custom payload
	Status     string `json:"status"`     // "success", "error", "partial"
	Error      string `json:"error"`      // error message when Status == "error"
	Duration   int64  `json:"duration"`   // execution time in milliseconds
	ExecutedAt int64  `json:"executedAt"` // unix millis timestamp
	Fix        string `json:"fix"`        // fix recommendation on failure
}

// Tool represents an executable capability the LLM can invoke.
type Tool interface {
	Name() string
	Description() string
	Parameters() any
	Execute(ctx context.Context, input map[string]any) (*Result, error)
}

// Ok builds a successful result.
func Ok(data any) *Result {
	return &Result{
		Data:       data,
		Status:     "success",
		ExecutedAt: time.Now().UnixMilli(),
	}
}

// Err builds an error result with a fix recommendation.
func Err(msg string, fix string) *Result {
	return &Result{
		Status:     "error",
		Error:      msg,
		Fix:        fix,
		ExecutedAt: time.Now().UnixMilli(),
	}
}

// Measure wraps a function call and auto-fills Duration and ExecutedAt.
func Measure(fn func() (*Result, error)) (*Result, error) {
	start := time.Now()
	res, err := fn()
	if err != nil {
		return nil, err
	}
	res.Duration = time.Since(start).Milliseconds()
	res.ExecutedAt = start.UnixMilli()
	return res, nil
}

// ExecuteFunc is the signature for a tool function used by FunctionTool.
type ExecuteFunc func(ctx context.Context, input map[string]any) (*Result, error)

// FunctionTool is a convenience adapter that wraps a Go function as a Tool.
type FunctionTool struct {
	name        string
	description string
	parameters  any
	fn          ExecuteFunc
}

func (t *FunctionTool) Name() string        { return t.name }
func (t *FunctionTool) Description() string { return t.description }
func (t *FunctionTool) Parameters() any     { return t.parameters }

func (t *FunctionTool) Execute(ctx context.Context, input map[string]any) (*Result, error) {
	return Measure(func() (*Result, error) {
		return t.fn(ctx, input)
	})
}

// NewFunctionTool creates a Tool from a Go function.
func NewFunctionTool(name, description string, parameters any, fn ExecuteFunc) *FunctionTool {
	return &FunctionTool{
		name:        name,
		description: description,
		parameters:  parameters,
		fn:          fn,
	}
}
