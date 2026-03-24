package skill

import (
	"errors"

	"github.com/ratrektlabs/rl-agent/tool"
)

var (
	ErrMissingName        = errors.New("skill name is required")
	ErrMissingDescription = errors.New("skill description is required")
	ErrMissingInstruction = errors.New("skill instruction is required")
)

type Skill interface {
	Name() string
	Description() string
	Instructions() string
	Tools() []tool.Tool
}

type SkillInfo struct {
	Name         string
	Description  string
	Instructions string
	Tools        []tool.Tool
}

type FuncSkill struct {
	name         string
	description  string
	instructions string
	tools        []tool.Tool
}

func (s *FuncSkill) Name() string         { return s.name }
func (s *FuncSkill) Description() string  { return s.description }
func (s *FuncSkill) Instructions() string { return s.instructions }
func (s *FuncSkill) Tools() []tool.Tool   { return s.tools }

type Builder struct {
	name         string
	description  string
	instructions string
	tools        []tool.Tool
	err          error
}

func New(name string) *Builder {
	return &Builder{name: name}
}

func (b *Builder) Description(desc string) *Builder {
	if b.err != nil {
		return b
	}
	b.description = desc
	return b
}

func (b *Builder) Instructions(instructions string) *Builder {
	if b.err != nil {
		return b
	}
	b.instructions = instructions
	return b
}

func (b *Builder) WithTool(t tool.Tool) *Builder {
	if b.err != nil {
		return b
	}
	b.tools = append(b.tools, t)
	return b
}

func (b *Builder) WithTools(tools ...tool.Tool) *Builder {
	if b.err != nil {
		return b
	}
	b.tools = append(b.tools, tools...)
	return b
}

func (b *Builder) Build() (Skill, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.name == "" {
		return nil, ErrMissingName
	}
	if b.description == "" {
		return nil, ErrMissingDescription
	}
	if b.instructions == "" {
		return nil, ErrMissingInstruction
	}
	return &FuncSkill{
		name:         b.name,
		description:  b.description,
		instructions: b.instructions,
		tools:        b.tools,
	}, nil
}

func (b *Builder) MustBuild() Skill {
	s, err := b.Build()
	if err != nil {
		panic(err)
	}
	return s
}

type Info struct {
	Name         string
	Description  string
	Instructions string
	Tools        []tool.Tool
}
