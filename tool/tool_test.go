package tool_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ratrektlabs/rakit/tool"
)

func TestOk(t *testing.T) {
	r := tool.Ok("payload")
	if r.Status != "success" {
		t.Fatalf("Status=%q want success", r.Status)
	}
	if r.Data != "payload" {
		t.Fatalf("Data=%v want payload", r.Data)
	}
	if r.ExecutedAt == 0 {
		t.Fatal("ExecutedAt not set")
	}
}

func TestErr(t *testing.T) {
	r := tool.Err("boom", "retry it")
	if r.Status != "error" {
		t.Fatalf("Status=%q want error", r.Status)
	}
	if r.Error != "boom" || r.Fix != "retry it" {
		t.Fatalf("Error/Fix mismatch: %+v", r)
	}
}

func TestMeasure(t *testing.T) {
	r, err := tool.Measure(func() (*tool.Result, error) {
		time.Sleep(5 * time.Millisecond)
		return tool.Ok("x"), nil
	})
	if err != nil {
		t.Fatalf("Measure err: %v", err)
	}
	if r.Duration < 1 {
		t.Fatalf("Duration=%d want >=1ms", r.Duration)
	}
	if r.ExecutedAt == 0 {
		t.Fatal("ExecutedAt not set")
	}
}

func TestMeasurePropagatesError(t *testing.T) {
	_, err := tool.Measure(func() (*tool.Result, error) {
		return nil, errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("want boom error, got %v", err)
	}
}

func TestFunctionTool(t *testing.T) {
	params := map[string]any{"type": "object"}
	ft := tool.NewFunctionTool(
		"echo",
		"echoes input",
		params,
		func(_ context.Context, in map[string]any) (*tool.Result, error) {
			return tool.Ok(in["msg"]), nil
		},
	)
	if ft.Name() != "echo" || ft.Description() != "echoes input" {
		t.Fatalf("metadata mismatch: %s / %s", ft.Name(), ft.Description())
	}
	got := ft.Parameters()
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("Parameters() want map[string]any got %T", got)
	}
	r, err := ft.Execute(context.Background(), map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if r.Data != "hi" {
		t.Fatalf("Data=%v want hi", r.Data)
	}
	if r.Duration < 0 {
		t.Fatalf("Duration=%d want non-negative", r.Duration)
	}
}

func TestRegistry(t *testing.T) {
	r := tool.NewRegistry()
	if r.Len() != 0 {
		t.Fatalf("fresh registry Len=%d want 0", r.Len())
	}
	ft := tool.NewFunctionTool("one", "d", nil, func(context.Context, map[string]any) (*tool.Result, error) {
		return tool.Ok(1), nil
	})
	r.Register(ft)
	if r.Len() != 1 {
		t.Fatalf("Len=%d want 1", r.Len())
	}
	if r.Get("one") == nil {
		t.Fatal("Get(one) returned nil")
	}
	if r.Get("missing") != nil {
		t.Fatal("Get(missing) want nil")
	}

	// Register same name again overwrites
	r.Register(tool.NewFunctionTool("one", "d2", nil, nil))
	if r.Len() != 1 {
		t.Fatalf("Len after overwrite=%d want 1", r.Len())
	}
	if r.Get("one").Description() != "d2" {
		t.Fatalf("overwrite did not replace tool")
	}

	// Schema returns provider.Tool
	r.Register(tool.NewFunctionTool("two", "d", map[string]any{"type": "object"}, nil))
	schemas := r.Schema()
	if len(schemas) != 2 {
		t.Fatalf("Schema len=%d want 2", len(schemas))
	}

	r.Unregister("one")
	if r.Get("one") != nil {
		t.Fatal("Unregister did not remove tool")
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := tool.NewRegistry()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			r.Register(tool.NewFunctionTool("t", "d", nil, nil))
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = r.Get("t")
		_ = r.All()
	}
	<-done
}
