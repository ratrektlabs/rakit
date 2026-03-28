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
