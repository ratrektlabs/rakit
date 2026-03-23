package rlagent

import (
	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/skill"
	"github.com/ratrektlabs/rl-agent/tool"
)

func Tool(name string) *tool.Builder {
	return tool.New(name)
}

func Skill(name string) *skill.Builder {
	return skill.New(name)
}

func Agent(p provider.Provider) *agent.Builder {
	return agent.NewBuilder(p)
}

func NewToolRegistry() *tool.Registry {
	return tool.NewRegistry()
}

func NewSkillRegistry() *skill.Registry {
	return skill.NewRegistry()
}

type AgentType = agent.Agent
type ToolInterface = tool.Tool
type SkillInterface = skill.Skill
type Provider = provider.Provider
type Builder = agent.Builder
type ToolBuilder = tool.Builder
type SkillBuilder = skill.Builder
