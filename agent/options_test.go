package agent

import (
	"testing"
)

func TestWithSystemPrompt(t *testing.T) {
	cfg := ApplyRunOptions(WithSystemPrompt("test prompt"))
	if cfg.SystemPrompt != "test prompt" {
		t.Errorf("expected 'test prompt', got %s", cfg.SystemPrompt)
	}
}

func TestWithTools(t *testing.T) {
	mt := &mockTool{}
	cfg := ApplyRunOptions(WithTools(mt))
	if len(cfg.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(cfg.Tools))
	}
}

func TestWithSkills(t *testing.T) {
	ms := &mockSkill{}
	cfg := ApplyRunOptions(WithSkills(ms))
	if len(cfg.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(cfg.Skills))
	}
}

func TestWithMaxSteps(t *testing.T) {
	cfg := ApplyRunOptions(WithMaxSteps(20))
	if cfg.MaxSteps != 20 {
		t.Errorf("expected 20, got %d", cfg.MaxSteps)
	}
}

func TestWithSession(t *testing.T) {
	cfg := ApplyRunOptions(WithSession("session-123"))
	if cfg.SessionID != "session-123" {
		t.Errorf("expected 'session-123', got %s", cfg.SessionID)
	}
}

func TestWithMemory(t *testing.T) {
	m := &mockMemory{}
	cfg := ApplyRunOptions(WithMemory(m))
	if cfg.Memory == nil {
		t.Error("expected memory to be set")
	}
}

func TestApplyRunOptions_Multiple(t *testing.T) {
	cfg := ApplyRunOptions(
		WithSystemPrompt("prompt"),
		WithMaxSteps(5),
		WithSession("sess"),
	)
	if cfg.SystemPrompt != "prompt" {
		t.Errorf("expected 'prompt', got %s", cfg.SystemPrompt)
	}
	if cfg.MaxSteps != 5 {
		t.Errorf("expected 5, got %d", cfg.MaxSteps)
	}
	if cfg.SessionID != "sess" {
		t.Errorf("expected 'sess', got %s", cfg.SessionID)
	}
}

func TestDefaultRunConfig(t *testing.T) {
	cfg := DefaultRunConfig()
	if cfg.MaxSteps != 10 {
		t.Errorf("expected MaxSteps 10, got %d", cfg.MaxSteps)
	}
}

func TestApplyRunOptions_Empty(t *testing.T) {
	cfg := ApplyRunOptions()
	if cfg.MaxSteps != 10 {
		t.Errorf("expected default MaxSteps 10, got %d", cfg.MaxSteps)
	}
}

type mockMemory struct{}

func (m *mockMemory) Get(key string) (any, bool) { return nil, false }
func (m *mockMemory) Set(key string, value any)  {}
