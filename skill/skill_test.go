package skill

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rl-agent/tool"
)

func TestSkill_Interface(t *testing.T) {
	var _ Skill = (*FuncSkill)(nil)
}

func TestFuncSkill_Accessors(t *testing.T) {
	s := &FuncSkill{
		name:         "test-skill",
		description:  "A test skill",
		instructions: "Do something useful",
		tools:        nil,
	}

	if s.Name() != "test-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "test-skill")
	}
	if s.Description() != "A test skill" {
		t.Errorf("Description() = %q, want %q", s.Description(), "A test skill")
	}
	if s.Instructions() != "Do something useful" {
		t.Errorf("Instructions() = %q, want %q", s.Instructions(), "Do something useful")
	}
	if s.Tools() != nil {
		t.Errorf("Tools() = %v, want nil", s.Tools())
	}
}

func TestBuilder_Build(t *testing.T) {
	s, err := New("calculator").
		Description("Performs calculations").
		Instructions("Use this to do math").
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if s.Name() != "calculator" {
		t.Errorf("Name() = %q, want %q", s.Name(), "calculator")
	}
}

func TestBuilder_Build_MissingName(t *testing.T) {
	_, err := New("").
		Description("A skill").
		Instructions("Instructions").
		Build()

	if err != ErrMissingName {
		t.Errorf("expected ErrMissingName, got %v", err)
	}
}

func TestBuilder_Build_MissingDescription(t *testing.T) {
	_, err := New("skill").
		Description("").
		Instructions("Instructions").
		Build()

	if err != ErrMissingDescription {
		t.Errorf("expected ErrMissingDescription, got %v", err)
	}
}

func TestBuilder_Build_MissingInstructions(t *testing.T) {
	_, err := New("skill").
		Description("A skill").
		Instructions("").
		Build()

	if err != ErrMissingInstruction {
		t.Errorf("expected ErrMissingInstruction, got %v", err)
	}
}

func TestBuilder_WithTool(t *testing.T) {
	testTool := &mockTool{name: "test-tool"}

	s, err := New("skill").
		Description("A skill").
		Instructions("Instructions").
		WithTool(testTool).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	tools := s.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name() != "test-tool" {
		t.Errorf("tool name = %q, want %q", tools[0].Name(), "test-tool")
	}
}

func TestBuilder_WithTools(t *testing.T) {
	tool1 := &mockTool{name: "tool-1"}
	tool2 := &mockTool{name: "tool-2"}

	s, err := New("skill").
		Description("A skill").
		Instructions("Instructions").
		WithTools(tool1, tool2).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	tools := s.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestBuilder_MustBuild(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustBuild to panic on invalid skill")
		}
	}()

	New("").MustBuild()
}

func TestBuilder_MustBuild_Success(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()

	s := New("skill").
		Description("A skill").
		Instructions("Instructions").
		MustBuild()

	if s.Name() != "skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "skill")
	}
}

func TestBuilder_Chaining(t *testing.T) {
	tool1 := &mockTool{name: "tool-1"}
	tool2 := &mockTool{name: "tool-2"}

	s, err := New("chained-skill").
		Description("A chained skill").
		Instructions("Instructions").
		WithTool(tool1).
		WithTools(tool2).
		Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if s.Name() != "chained-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "chained-skill")
	}
	if len(s.Tools()) != 2 {
		t.Errorf("expected 2 tools, got %d", len(s.Tools()))
	}
}

type mockTool struct {
	name string
}

func (m *mockTool) Name() string                                { return m.name }
func (m *mockTool) Description() string                         { return "mock tool" }
func (m *mockTool) Parameters() map[string]tool.ParameterSchema { return nil }
func (m *mockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil
}
