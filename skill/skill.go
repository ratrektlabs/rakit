package skill

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ratrektlabs/rl-agent/tool"
)

type ApplicationCommandOptionType int

const (
	OptionTypeString ApplicationCommandOptionType = iota + 1
	OptionTypeInteger
	OptionTypeBoolean
	OptionTypeUser
	OptionTypeChannel
	OptionTypeRole
	OptionTypeMentionable
	OptionTypeNumber
	OptionTypeAttachment
)

type SlashCommandChoice struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

type SlashCommandOption struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Type        ApplicationCommandOptionType `json:"type"`
	Required    bool                         `json:"required"`
	Choices     []SlashCommandChoice         `json:"choices,omitempty"`
}

type SlashCommandDefinition struct {
	Name              string               `json:"name"`
	Description       string               `json:"description"`
	Options           []SlashCommandOption `json:"options,omitempty"`
	DefaultPermission bool                 `json:"default_permission"`
	NSFW              bool                 `json:"nsfw"`
}

type Skill interface {
	Name() string
	Description() string
	Tools() []tool.Tool
	Instructions() string
	SlashCommand() *SlashCommandDefinition
}

type skillImpl struct {
	name         string
	description  string
	tools        []tool.Tool
	instructions string
	slashCommand *SlashCommandDefinition
}

func (s *skillImpl) Name() string {
	return s.name
}

func (s *skillImpl) Description() string {
	return s.description
}

func (s *skillImpl) Tools() []tool.Tool {
	return s.tools
}

func (s *skillImpl) Instructions() string {
	return s.instructions
}

func (s *skillImpl) SlashCommand() *SlashCommandDefinition {
	return s.slashCommand
}

type Builder struct {
	name         string
	description  string
	tools        []tool.Tool
	instructions string
	slashCommand *SlashCommandDefinition
}

func New(name string) *Builder {
	return &Builder{
		name:  name,
		tools: make([]tool.Tool, 0),
	}
}

func (s *Builder) WithDescription(desc string) *Builder {
	s.description = desc
	return s
}

func (s *Builder) WithTool(t tool.Tool) *Builder {
	s.tools = append(s.tools, t)
	return s
}

func (s *Builder) WithTools(tools ...tool.Tool) *Builder {
	s.tools = append(s.tools, tools...)
	return s
}

func (s *Builder) WithInstruction(instruction string) *Builder {
	s.instructions = instruction
	return s
}

func (s *Builder) AsSlashCommand(name, description string) *Builder {
	s.slashCommand = &SlashCommandDefinition{
		Name:              name,
		Description:       description,
		DefaultPermission: true,
	}
	return s
}

func (s *Builder) WithSlashOption(name, description string, optionType ApplicationCommandOptionType, required bool) *Builder {
	if s.slashCommand == nil {
		return s
	}
	s.slashCommand.Options = append(s.slashCommand.Options, SlashCommandOption{
		Name:        name,
		Description: description,
		Type:        optionType,
		Required:    required,
	})
	return s
}

func (s *Builder) WithSlashChoice(optionName string, choiceName string, choiceValue interface{}) *Builder {
	if s.slashCommand == nil {
		return s
	}
	for i, opt := range s.slashCommand.Options {
		if opt.Name == optionName {
			s.slashCommand.Options[i].Choices = append(opt.Choices, SlashCommandChoice{
				Name:  choiceName,
				Value: choiceValue,
			})
			break
		}
	}
	return s
}

func (s *Builder) WithSlashDefaultPermission(defaultPerm bool) *Builder {
	if s.slashCommand != nil {
		s.slashCommand.DefaultPermission = defaultPerm
	}
	return s
}

func (s *Builder) WithSlashNSFW(nsfw bool) *Builder {
	if s.slashCommand != nil {
		s.slashCommand.NSFW = nsfw
	}
	return s
}

func (s *Builder) Build() (Skill, error) {
	if s.name == "" {
		return nil, errors.New("skill name is required")
	}
	return &skillImpl{
		name:         s.name,
		description:  s.description,
		tools:        s.tools,
		instructions: s.instructions,
		slashCommand: s.slashCommand,
	}, nil
}

func (s *Builder) MustBuild() Skill {
	skill, err := s.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build skill: %v", err))
	}
	return skill
}

type FluentSkill = Builder
type skillBuilder = Builder

func (s *Builder) WithFluentTool(t *tool.Builder) *Builder {
	built, err := t.Build()
	if err != nil {
		return s
	}
	s.tools = append(s.tools, built)
	return s
}

type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]Skill),
	}
}

func (r *Registry) Register(s interface{}) error {
	var skill Skill
	switch v := s.(type) {
	case Skill:
		skill = v
	case *Builder:
		built, err := v.Build()
		if err != nil {
			return err
		}
		skill = built
	default:
		return errors.New("skill must implement Skill interface or be *Builder")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := skill.Name()
	if name == "" {
		return errors.New("skill name cannot be empty")
	}

	if _, exists := r.skills[name]; exists {
		return fmt.Errorf("skill %q already registered", name)
	}

	r.skills[name] = skill
	return nil
}

func (r *Registry) MustRegister(s interface{}) {
	if err := r.Register(s); err != nil {
		panic(fmt.Sprintf("failed to register skill: %v", err))
	}
}

func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[name]; !exists {
		return fmt.Errorf("skill %q not found", name)
	}

	delete(r.skills, name)
	return nil
}

func (r *Registry) Get(name string) (Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, exists := r.skills[name]
	if !exists {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	return s, nil
}

func (r *Registry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		result = append(result, s)
	}
	return result
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0, len(r.skills))
	for name := range r.skills {
		result = append(result, name)
	}
	return result
}

func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills = make(map[string]Skill)
}

type FuncSkill struct {
	name         string
	description  string
	tools        []tool.Tool
	instructions string
	slashCommand *SlashCommandDefinition
}

func NewFuncSkill(name string, fn func(ctx context.Context, params map[string]any) (any, error)) *FuncSkill {
	return &FuncSkill{
		name: name,
		tools: []tool.Tool{
			&toolFunc{
				name:        name + "_execute",
				description: "Execute the " + name + " skill",
				parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
				execute: fn,
			},
		},
	}
}

func (s *FuncSkill) WithDescription(desc string) *FuncSkill {
	s.description = desc
	return s
}

func (s *FuncSkill) WithInstruction(instruction string) *FuncSkill {
	s.instructions = instruction
	return s
}

func (s *FuncSkill) Skill() Skill {
	return &skillImpl{
		name:         s.name,
		description:  s.description,
		tools:        s.tools,
		instructions: s.instructions,
		slashCommand: s.slashCommand,
	}
}

type toolFunc struct {
	name        string
	description string
	parameters  map[string]interface{}
	execute     func(ctx context.Context, params map[string]any) (any, error)
}

func (t *toolFunc) Name() string                       { return t.name }
func (t *toolFunc) Description() string                { return t.description }
func (t *toolFunc) Parameters() map[string]interface{} { return t.parameters }
func (t *toolFunc) Execute(ctx context.Context, params map[string]any) (any, error) {
	return t.execute(ctx, params)
}
