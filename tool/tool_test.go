package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestToolInterface(t *testing.T) {
	tool := New("test").
		Description("A test tool").
		Param("query", TypeString, "Search query", true).
		Action(func(ctx context.Context, args map[string]any) (any, error) {
			return "result: " + args["query"].(string), nil
		}).MustBuild()

	if tool.Name() != "test" {
		t.Errorf("expected name 'test', got %s", tool.Name())
	}
	if tool.Description() != "A test tool" {
		t.Errorf("expected description 'A test tool', got %s", tool.Description())
	}
}

func TestToolBuilder_Description(t *testing.T) {
	tool := New("test").Description("Test description").Action(func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}).MustBuild()
	if tool.Description() != "Test description" {
		t.Errorf("expected 'Test description', got %s", tool.Description())
	}
}

func TestToolBuilder_Param(t *testing.T) {
	tool := New("test").
		Description("Test tool").
		Param("query", TypeString, "Search query", true).
		Param("limit", TypeInteger, "Max results", false).
		Action(func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		}).MustBuild()

	params := tool.Parameters()
	if len(params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(params))
	}
	queryParam, exists := params["query"]
	if !exists {
		t.Fatal("expected 'query' parameter to exist")
	}
	if !queryParam.Required {
		t.Error("expected query to be required")
	}
	limitParam, exists := params["limit"]
	if !exists {
		t.Fatal("expected 'limit' parameter to exist")
	}
	if limitParam.Required {
		t.Error("expected limit to be optional")
	}
}

func TestToolBuilder_Action(t *testing.T) {
	executed := false
	tool := New("test").
		Description("Test tool").
		Action(func(ctx context.Context, args map[string]any) (any, error) {
			executed = true
			return "done", nil
		}).MustBuild()

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected action to be executed")
	}
	if result != "done" {
		t.Errorf("expected 'done', got %v", result)
	}
}

func TestToolBuilder_MustBuild(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from MustBuild")
		}
	}()

	New("test").MustBuild() // Should panic - no action
}

func TestToolRegistry_Register(t *testing.T) {
	registry := NewRegistry()

	tool := New("test").
		Description("Test tool").
		Action(func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		}).MustBuild()

	if err := registry.Register(tool); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := registry.Get("test"); err != nil {
		t.Errorf("expected to find tool 'test': %v", err)
	}
}

func TestToolRegistry_Get(t *testing.T) {
	registry := NewRegistry()

	tool := New("test").Description("Test tool").Action(func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}).MustBuild()
	_ = registry.Register(tool)

	retrieved, err := registry.Get("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved.Name() != "test" {
		t.Errorf("expected name 'test', got %s", retrieved.Name())
	}

	_, err = registry.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

func TestToolRegistry_List(t *testing.T) {
	registry := NewRegistry()

	tool1 := New("tool1").Description("Test tool 1").Action(func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}).MustBuild()
	tool2 := New("tool2").Description("Test tool 2").Action(func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}).MustBuild()

	_ = registry.Register(tool1)
	_ = registry.Register(tool2)

	list := registry.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, info := range list {
		names[info.Name] = true
	}
	if !names["tool1"] || !names["tool2"] {
		t.Error("expected both tool1 and tool2 in list")
	}
}

func TestToolRegistry_ToProviderTools(t *testing.T) {
	registry := NewRegistry()

	tool := New("calculator").
		Description("Perform calculations").
		Param("expression", TypeString, "Math expression", true).
		Action(func(ctx context.Context, args map[string]any) (any, error) {
			return 42, nil
		}).MustBuild()
	_ = registry.Register(tool)

	defs := registry.ToProviderTools()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(defs))
	}
	if defs[0].Type != "function" {
		t.Errorf("expected type 'function', got %s", defs[0].Type)
	}
	if defs[0].Function.Name != "calculator" {
		t.Errorf("expected function name 'calculator', got %s", defs[0].Function.Name)
	}
}

func TestToolBuilder_BuildTwice(t *testing.T) {
	builder := New("test").
		Description("Test").
		Action(func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		})

	tool1, err1 := builder.Build()
	if err1 != nil {
		t.Fatalf("unexpected error: %v", err1)
	}

	tool2, err2 := builder.Build()
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}

	if tool1.Name() != tool2.Name() {
		t.Error("expected tools to have same name")
	}
	if tool1.Description() != tool2.Description() {
		t.Error("expected tools to have same description")
	}
}

func TestToolRegistry_DuplicateRegister(t *testing.T) {
	registry := NewRegistry()

	tool := New("test").Description("Test tool").Action(func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}).MustBuild()

	if err := registry.Register(tool); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Register same tool again - should error
	if err := registry.Register(tool); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestToolRegistry_Concurrency(t *testing.T) {
	registry := NewRegistry()

	// Register multiple tools concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			tool := New("tool_" + string(rune('0'+id))).Description("Test tool").Action(func(ctx context.Context, args map[string]any) (any, error) {
				return nil, nil
			}).MustBuild()
			_ = registry.Register(tool)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// List should have all tools
	list := registry.List()
	if len(list) != 10 {
		t.Errorf("expected 10 tools, got %d", len(list))
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Parameter: "test_param",
		Message:   "test message",
	}
	if !strings.Contains(err.Error(), "test_param") {
		t.Error("expected error string to contain parameter name")
	}
	if !strings.Contains(err.Error(), "test message") {
		t.Error("expected error string to contain message")
	}
}

func TestValidationError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &ValidationError{
		Parameter: "test_param",
		Message:   "test message",
		Err:       underlying,
	}
	if !errors.Is(err, underlying) {
		t.Error("expected errors.Is to match underlying error")
	}
}

func TestSC005_MockToolInterface(t *testing.T) {
	// SC-005: All interfaces mockable
	var _ Tool = New("mock").Description("Mock tool").Action(func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}).MustBuild()

	var _ ToolRegistry = NewRegistry()
}
