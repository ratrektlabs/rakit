package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ratrektlabs/rl-agent/memory"
	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/skill"
	"github.com/ratrektlabs/rl-agent/tool"
)

type AgentState string

const (
	StateIdle     AgentState = "idle"
	StateRunning  AgentState = "running"
	StateFinished AgentState = "finished"
	StateError    AgentState = "error"
)

type RunInput struct {
	Messages  []provider.Message `json:"messages"`
	SessionID string             `json:"session_id,omitempty"`
	UserID    string             `json:"user_id,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
	MaxSteps  int                `json:"max_steps,omitempty"`
}

type RunOutput struct {
	Message     provider.Message      `json:"message"`
	Steps       int                   `json:"steps"`
	Usage       provider.Usage        `json:"usage"`
	State       AgentState            `json:"state"`
	ToolResults []ToolExecutionResult `json:"tool_results,omitempty"`
}

type ToolExecutionResult struct {
	ToolName string `json:"tool_name"`
	Success  bool   `json:"success"`
	Result   any    `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

type Agent struct {
	mu       sync.RWMutex
	provider provider.Provider
	tools    *tool.Registry
	memory   memory.Memory
	model    string
	state    AgentState
	options  *Options
}

type Options struct {
	SystemPrompt string
	Temperature  float64
	MaxTokens    int
	MaxSteps     int
	Hooks        Hooks
}

type Hooks struct {
	BeforeStep func(ctx context.Context, step int, messages []provider.Message) error
	AfterStep  func(ctx context.Context, step int, output *RunOutput) error
	OnToolCall func(ctx context.Context, toolName string, params map[string]any) error
}

type Option func(*Options)

func WithSystemPrompt(prompt string) Option {
	return func(o *Options) {
		o.SystemPrompt = prompt
	}
}

func WithTemperature(temp float64) Option {
	return func(o *Options) {
		o.Temperature = temp
	}
}

func WithMaxTokens(tokens int) Option {
	return func(o *Options) {
		o.MaxTokens = tokens
	}
}

func WithMaxSteps(steps int) Option {
	return func(o *Options) {
		o.MaxSteps = steps
	}
}

func WithHooks(hooks Hooks) Option {
	return func(o *Options) {
		o.Hooks = hooks
	}
}

func New(p provider.Provider, opts ...Option) *Agent {
	options := &Options{
		Temperature: 0.7,
		MaxTokens:   4096,
		MaxSteps:    10,
	}

	for _, opt := range opts {
		opt(options)
	}

	return &Agent{
		provider: p,
		tools:    tool.NewRegistry(),
		state:    StateIdle,
		options:  options,
	}
}

func (a *Agent) WithTools(r *tool.Registry) *Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tools = r
	return a
}

func (a *Agent) WithMemory(m memory.Memory) *Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.memory = m
	return a
}

func (a *Agent) WithModel(model string) *Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = model
	return a
}

func (a *Agent) State() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *Agent) Run(ctx context.Context, input RunInput) (*RunOutput, error) {
	a.mu.Lock()
	a.state = StateRunning
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		if a.state == StateRunning {
			a.state = StateFinished
		}
		a.mu.Unlock()
	}()

	maxSteps := a.options.MaxSteps
	if input.MaxSteps > 0 {
		maxSteps = input.MaxSteps
	}

	messages := a.prepareMessages(input.Messages)

	output := &RunOutput{
		State: StateRunning,
	}

	for step := 1; step <= maxSteps; step++ {
		if a.options.Hooks.BeforeStep != nil {
			if err := a.options.Hooks.BeforeStep(ctx, step, messages); err != nil {
				return nil, fmt.Errorf("before step hook failed: %w", err)
			}
		}

		req := a.buildRequest(messages)

		resp, err := a.provider.Complete(ctx, req)
		if err != nil {
			a.mu.Lock()
			a.state = StateError
			a.mu.Unlock()
			return nil, fmt.Errorf("completion failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, errors.New("no choices in response")
		}

		assistantMsg := resp.Choices[0].Message
		messages = append(messages, assistantMsg)

		output.Message = assistantMsg
		output.Steps = step
		output.Usage = resp.Usage

		if len(assistantMsg.ToolCalls) > 0 {
			toolResults, continueRun, err := a.executeToolCalls(ctx, assistantMsg.ToolCalls)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed: %w", err)
			}

			output.ToolResults = toolResults

			if !continueRun {
				output.State = StateFinished
				break
			}

			for _, result := range toolResults {
				resultContent := ""
				if result.Success {
					if b, err := json.Marshal(result.Result); err == nil {
						resultContent = string(b)
					}
				} else {
					resultContent = result.Error
				}

				messages = append(messages, provider.Message{
					Role:       provider.RoleTool,
					Content:    resultContent,
					ToolCallID: result.ToolName,
				})
			}
		} else {
			output.State = StateFinished
			break
		}

		if a.options.Hooks.AfterStep != nil {
			if err := a.options.Hooks.AfterStep(ctx, step, output); err != nil {
				return nil, fmt.Errorf("after step hook failed: %w", err)
			}
		}
	}

	if a.memory != nil && input.UserID != "" && input.SessionID != "" {
		for _, msg := range messages {
			entry := memory.Entry{
				Role:    string(msg.Role),
				Content: msg.Content,
			}
			_ = a.memory.Add(ctx, input.UserID, input.SessionID, entry)
		}
	}

	return output, nil
}

func (a *Agent) RunStream(ctx context.Context, input RunInput) (<-chan StreamEvent, error) {
	if !a.provider.SupportsStreaming() {
		return nil, errors.New("provider does not support streaming")
	}

	eventChan := make(chan StreamEvent, 100)

	go func() {
		defer close(eventChan)

		a.mu.Lock()
		a.state = StateRunning
		a.mu.Unlock()

		defer func() {
			a.mu.Lock()
			if a.state == StateRunning {
				a.state = StateFinished
			}
			a.mu.Unlock()
		}()

		maxSteps := a.options.MaxSteps
		if input.MaxSteps > 0 {
			maxSteps = input.MaxSteps
		}

		messages := a.prepareMessages(input.Messages)

		for step := 1; step <= maxSteps; step++ {
			eventChan <- StreamEvent{Type: StreamEventTypeStepStart, Step: step}

			req := a.buildRequest(messages)

			providerEvents, err := a.provider.Stream(ctx, req)
			if err != nil {
				eventChan <- StreamEvent{Type: StreamEventTypeError, Error: err}
				return
			}

			var fullContent string
			var toolCalls []provider.ToolCall

			for event := range providerEvents {
				switch event.Type {
				case provider.StreamEventContentDelta:
					fullContent += event.Delta
					eventChan <- StreamEvent{
						Type:  StreamEventTypeContentDelta,
						Delta: event.Delta,
					}
				case provider.StreamEventToolCall:
					toolCalls = append(toolCalls, *event.ToolCall)
					eventChan <- StreamEvent{
						Type:     StreamEventTypeToolCall,
						ToolCall: event.ToolCall,
					}
				case provider.StreamEventError:
					eventChan <- StreamEvent{Type: StreamEventTypeError, Error: event.Error}
					return
				case provider.StreamEventDone:
					eventChan <- StreamEvent{Type: StreamEventTypeStepEnd, Step: step}
				}
			}

			assistantMsg := provider.Message{
				Role:      provider.RoleAssistant,
				Content:   fullContent,
				ToolCalls: toolCalls,
			}
			messages = append(messages, assistantMsg)

			if len(toolCalls) > 0 {
				toolResults, continueRun, err := a.executeToolCalls(ctx, toolCalls)
				if err != nil {
					eventChan <- StreamEvent{Type: StreamEventTypeError, Error: err}
					return
				}

				for _, result := range toolResults {
					eventChan <- StreamEvent{
						Type:       StreamEventTypeToolResult,
						ToolResult: &result,
					}

					resultContent := ""
					if result.Success {
						if b, err := json.Marshal(result.Result); err == nil {
							resultContent = string(b)
						}
					} else {
						resultContent = result.Error
					}

					messages = append(messages, provider.Message{
						Role:       provider.RoleTool,
						Content:    resultContent,
						ToolCallID: result.ToolName,
					})
				}

				if !continueRun {
					break
				}
			} else {
				break
			}
		}

		eventChan <- StreamEvent{Type: StreamEventTypeFinished}
	}()

	return eventChan, nil
}

type StreamEventType string

const (
	StreamEventTypeStepStart    StreamEventType = "step_start"
	StreamEventTypeStepEnd      StreamEventType = "step_end"
	StreamEventTypeContentDelta StreamEventType = "content_delta"
	StreamEventTypeToolCall     StreamEventType = "tool_call"
	StreamEventTypeToolResult   StreamEventType = "tool_result"
	StreamEventTypeFinished     StreamEventType = "finished"
	StreamEventTypeError        StreamEventType = "error"
)

type StreamEvent struct {
	Type       StreamEventType      `json:"type"`
	Step       int                  `json:"step,omitempty"`
	Delta      string               `json:"delta,omitempty"`
	ToolCall   *provider.ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolExecutionResult `json:"tool_result,omitempty"`
	Error      error                `json:"error,omitempty"`
}

func (a *Agent) prepareMessages(inputMessages []provider.Message) []provider.Message {
	messages := make([]provider.Message, 0, len(inputMessages)+1)

	if a.options.SystemPrompt != "" {
		messages = append(messages, provider.Message{
			Role:    provider.RoleSystem,
			Content: a.options.SystemPrompt,
		})
	}

	messages = append(messages, inputMessages...)
	return messages
}

func (a *Agent) buildRequest(messages []provider.Message) *provider.CompletionRequest {
	req := &provider.CompletionRequest{
		Messages:  messages,
		Model:     a.model,
		MaxTokens: a.options.MaxTokens,
	}

	temp := a.options.Temperature
	req.Temperature = &temp

	if a.tools != nil && len(a.tools.List()) > 0 && a.provider.SupportsTools() {
		req.Tools = a.tools.ToProviderTools()
	}

	return req
}

func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []provider.ToolCall) ([]ToolExecutionResult, bool, error) {
	results := make([]ToolExecutionResult, 0, len(toolCalls))
	continueRun := true

	for _, tc := range toolCalls {
		var params map[string]any
		if len(tc.Function.Arguments) > 0 {
			if err := json.Unmarshal(tc.Function.Arguments, &params); err != nil {
				params = make(map[string]any)
			}
		}

		if a.options.Hooks.OnToolCall != nil {
			if err := a.options.Hooks.OnToolCall(ctx, tc.Function.Name, params); err != nil {
				return nil, false, fmt.Errorf("tool call hook failed: %w", err)
			}
		}

		result := ToolExecutionResult{
			ToolName: tc.Function.Name,
		}

		if a.tools == nil {
			result.Success = false
			result.Error = "no tool registry configured"
			results = append(results, result)
			continue
		}

		t, err := a.tools.Get(tc.Function.Name)
		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("tool not found: %s", tc.Function.Name)
			results = append(results, result)
			continue
		}

		output, err := t.Execute(ctx, params)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = true
			result.Result = output

			if stop, ok := output.(StopSignal); ok && stop.Stop {
				continueRun = false
			}
		}

		results = append(results, result)
	}

	return results, continueRun, nil
}

type StopSignal struct {
	Stop bool `json:"stop"`
}

func Stop() StopSignal {
	return StopSignal{Stop: true}
}

type Builder struct {
	provider     provider.Provider
	model        string
	systemPrompt string
	temperature  float64
	maxTokens    int
	maxSteps     int
	tools        *tool.Registry
	skills       *skill.Registry
	memory       memory.Memory
	hooks        Hooks
}

func NewBuilder(p provider.Provider) *Builder {
	return &Builder{
		provider:    p,
		temperature: 0.7,
		maxTokens:   4096,
		maxSteps:    10,
		tools:       tool.NewRegistry(),
		skills:      skill.NewRegistry(),
	}
}

func (b *Builder) WithModel(model string) *Builder {
	b.model = model
	return b
}

func (b *Builder) WithSystemPrompt(prompt string) *Builder {
	b.systemPrompt = prompt
	return b
}

func (b *Builder) WithTemperature(temp float64) *Builder {
	b.temperature = temp
	return b
}

func (b *Builder) WithMaxTokens(tokens int) *Builder {
	b.maxTokens = tokens
	return b
}

func (b *Builder) WithMaxSteps(steps int) *Builder {
	b.maxSteps = steps
	return b
}

func (b *Builder) WithToolRegistry(registry *tool.Registry) *Builder {
	b.tools = registry
	return b
}

func (b *Builder) WithToolFromRegistry(registry *tool.Registry, name string) *Builder {
	if registry == nil {
		return b
	}
	t, err := registry.Get(name)
	if err != nil {
		return b
	}
	_ = b.tools.Register(t)
	return b
}

func (b *Builder) WithToolsFromRegistry(registry *tool.Registry, names ...string) *Builder {
	if registry == nil {
		return b
	}
	for _, name := range names {
		b.WithToolFromRegistry(registry, name)
	}
	return b
}

func (b *Builder) WithToolFunc(name, description string, params map[string]interface{}, fn func(ctx context.Context, params map[string]any) (any, error)) *Builder {
	_ = b.tools.RegisterFunc(name, description, params, fn)
	return b
}

func (b *Builder) WithTool(t interface{}) *Builder {
	_ = b.tools.Register(t)
	return b
}

func (b *Builder) WithSkillRegistry(registry *skill.Registry) *Builder {
	b.skills = registry
	return b
}

func (b *Builder) WithSkillFromRegistry(registry *skill.Registry, name string) *Builder {
	if registry == nil {
		return b
	}
	s, err := registry.Get(name)
	if err != nil {
		return b
	}
	_ = b.skills.Register(s)
	for _, t := range s.Tools() {
		_ = b.tools.Register(t)
	}
	return b
}

func (b *Builder) WithSkillsFromRegistry(registry *skill.Registry, names ...string) *Builder {
	for _, name := range names {
		b.WithSkillFromRegistry(registry, name)
	}
	return b
}

func (b *Builder) WithSkill(s interface{}) *Builder {
	if err := b.skills.Register(s); err != nil {
		return b
	}
	// Also register skill's tools
	var skillImpl skill.Skill
	switch v := s.(type) {
	case skill.Skill:
		skillImpl = v
	case *skill.Builder:
		built, err := v.Build()
		if err != nil {
			return b
		}
		skillImpl = built
	}
	if skillImpl != nil {
		for _, t := range skillImpl.Tools() {
			_ = b.tools.Register(t)
		}
	}
	return b
}



func (b *Builder) WithMemory(m memory.Memory) *Builder {
	b.memory = m
	return b
}

func (b *Builder) WithHooks(hooks Hooks) *Builder {
	b.hooks = hooks
	return b
}

func (b *Builder) Build() *Agent {
	return &Agent{
		provider: b.provider,
		tools:    b.tools,
		memory:   b.memory,
		model:    b.model,
		state:    StateIdle,
		options: &Options{
			SystemPrompt: b.systemPrompt,
			Temperature:  b.temperature,
			MaxTokens:    b.maxTokens,
			MaxSteps:     b.maxSteps,
			Hooks:        b.hooks,
		},
	}
}

func (a *Agent) AddTool(t interface{}) error {
	return a.tools.Register(t)
}

func (a *Agent) AddToolFunc(name, description string, params map[string]interface{}, fn func(ctx context.Context, params map[string]any) (any, error)) error {
	return a.tools.RegisterFunc(name, description, params, fn)
}

func (a *Agent) AddSkill(s interface{}) error {
	var skillImpl skill.Skill
	switch v := s.(type) {
	case skill.Skill:
		skillImpl = v
	case *skill.Builder:
		built, err := v.Build()
		if err != nil {
			return err
		}
		skillImpl = built
	default:
		return errors.New("invalid skill type")
	}
	
	for _, t := range skillImpl.Tools() {
		if err := a.tools.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) AddFluentSkill(s *skill.Builder) error {
	return a.AddSkill(s)
}

func (a *Agent) GetToolRegistry() *tool.Registry {
	return a.tools
}

func (a *Agent) GetModel() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}
