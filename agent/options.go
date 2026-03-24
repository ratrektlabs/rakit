package agent

import (
	"github.com/ratrektlabs/rl-agent/tool"
)

type RunOption func(*RunConfig)

type RunConfig struct {
	SystemPrompt string
	Tools        []tool.Tool
	Skills       []Skill
	MaxSteps     int
	SessionID    string
	Memory       Memory
}

type Memory interface {
	Get(key string) (any, bool)
	Set(key string, value any)
}

func WithSystemPrompt(prompt string) RunOption {
	return func(c *RunConfig) {
		c.SystemPrompt = prompt
	}
}

func WithTools(tools ...tool.Tool) RunOption {
	return func(c *RunConfig) {
		c.Tools = append(c.Tools, tools...)
	}
}

func WithSkills(skills ...Skill) RunOption {
	return func(c *RunConfig) {
		c.Skills = append(c.Skills, skills...)
	}
}

func WithMaxSteps(max int) RunOption {
	return func(c *RunConfig) {
		c.MaxSteps = max
	}
}

func WithSession(sessionID string) RunOption {
	return func(c *RunConfig) {
		c.SessionID = sessionID
	}
}

func WithMemory(memory Memory) RunOption {
	return func(c *RunConfig) {
		c.Memory = memory
	}
}

func DefaultRunConfig() *RunConfig {
	return &RunConfig{
		MaxSteps: 10,
	}
}

func ApplyRunOptions(opts ...RunOption) *RunConfig {
	cfg := DefaultRunConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
