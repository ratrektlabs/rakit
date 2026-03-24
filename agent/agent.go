package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/ratrektlabs/rl-agent/provider"
	"github.com/ratrektlabs/rl-agent/tool"
)

var (
	ErrMaxStepsExceeded = errors.New("max steps exceeded")
	ErrNoProvider       = errors.New("no provider configured")
)

type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonToolCalls FinishReason = "tool_calls"
	FinishReasonMaxSteps  FinishReason = "max_steps"
	FinishReasonError     FinishReason = "error"
	FinishReasonCancelled FinishReason = "cancelled"
)

type RunResult struct {
	Content      string
	ToolCalls    []provider.ToolCall
	FinishReason FinishReason
	Steps        int
	Usage        provider.UsageStats
}

type StreamEvent struct {
	Type         StreamEventType
	Delta        string
	ToolCall     *provider.ToolCall
	ToolResult   *ToolResultEvent
	Step         int
	FinishReason FinishReason
	Error        error
}

type StreamEventType string

const (
	StreamEventContentDelta  StreamEventType = "content_delta"
	StreamEventToolCallStart StreamEventType = "tool_call_start"
	StreamEventToolCallEnd   StreamEventType = "tool_call_end"
	StreamEventToolResult    StreamEventType = "tool_result"
	StreamEventStepStart     StreamEventType = "step_start"
	StreamEventStepEnd       StreamEventType = "step_end"
	StreamEventError         StreamEventType = "error"
	StreamEventDone          StreamEventType = "done"
)

type ToolResultEvent struct {
	Name   string
	Input  map[string]any
	Output any
	Error  error
}

type Agent interface {
	Run(ctx context.Context, messages []provider.Message) (*RunResult, error)
	Stream(ctx context.Context, messages []provider.Message) (<-chan StreamEvent, error)
	AddTool(t tool.Tool) error
	AddSkill(skill Skill) error
}

type Skill interface {
	Name() string
	Description() string
	Prompt() string
	Tools() []tool.Tool
}

type Config struct {
	Provider     provider.Provider
	SystemPrompt string
	MaxSteps     int
	Model        string
	Temperature  *float64
	MaxTokens    int
}

func DefaultConfig(p provider.Provider) *Config {
	temp := 0.7
	return &Config{
		Provider:     p,
		SystemPrompt: "",
		MaxSteps:     10,
		Model:        "",
		Temperature:  &temp,
		MaxTokens:    4096,
	}
}

func New(cfg *Config) Agent {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 10
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Temperature == nil {
		temp := 0.7
		cfg.Temperature = &temp
	}
	return &agent{
		config: cfg,
		tools:  tool.NewRegistry(),
		skills: make(map[string]Skill),
	}
}

type agent struct {
	config *Config
	tools  tool.ToolRegistry
	skills map[string]Skill
}

func (a *agent) Run(ctx context.Context, messages []provider.Message) (*RunResult, error) {
	if a.config.Provider == nil {
		return nil, ErrNoProvider
	}

	result := &RunResult{
		FinishReason: FinishReasonStop,
	}

	allMessages := a.buildMessages(messages)

	for step := 1; step <= a.config.MaxSteps; step++ {
		result.Steps = step

		req := provider.CompletionRequest{
			Model:       a.config.Model,
			Messages:    allMessages,
			Tools:       a.tools.ToProviderTools(),
			Temperature: a.config.Temperature,
			MaxTokens:   a.config.MaxTokens,
		}

		resp, err := a.config.Provider.Complete(ctx, req)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				result.FinishReason = FinishReasonCancelled
				return result, nil
			}
			result.FinishReason = FinishReasonError
			return result, fmt.Errorf("provider error: %w", err)
		}

		result.Usage.PromptTokens += resp.Usage.PromptTokens
		result.Usage.CompletionTokens += resp.Usage.CompletionTokens
		result.Usage.TotalTokens += resp.Usage.TotalTokens
		result.Content = resp.Content

		if len(resp.ToolCalls) == 0 {
			result.FinishReason = FinishReasonStop
			return result, nil
		}

		result.ToolCalls = resp.ToolCalls
		allMessages = append(allMessages, provider.Message{
			Role:      provider.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			t, err := a.tools.Get(tc.Name)
			if err != nil {
				allMessages = append(allMessages, provider.Message{
					Role:       provider.RoleTool,
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: tool not found: %s", tc.Name),
				})
				continue
			}

			toolResult, err := t.Execute(ctx, tc.Arguments)
			if err != nil {
				allMessages = append(allMessages, provider.Message{
					Role:       provider.RoleTool,
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: %v", err),
				})
				continue
			}

			allMessages = append(allMessages, provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("%v", toolResult),
			})
		}
	}

	result.FinishReason = FinishReasonMaxSteps
	return result, nil
}

func (a *agent) buildMessages(messages []provider.Message) []provider.Message {
	var allMessages []provider.Message

	if a.config.SystemPrompt != "" {
		allMessages = append(allMessages, provider.Message{
			Role:    provider.RoleSystem,
			Content: a.config.SystemPrompt,
		})
	}

	allMessages = append(allMessages, messages...)
	return allMessages
}

func (a *agent) Stream(ctx context.Context, messages []provider.Message) (<-chan StreamEvent, error) {
	if a.config.Provider == nil {
		ch := make(chan StreamEvent)
		close(ch)
		return ch, ErrNoProvider
	}

	out := make(chan StreamEvent, 64)

	go func() {
		defer close(out)

		allMessages := a.buildMessages(messages)

		for step := 1; step <= a.config.MaxSteps; step++ {
			select {
			case out <- StreamEvent{Type: StreamEventStepStart, Step: step}:
			case <-ctx.Done():
				out <- StreamEvent{Type: StreamEventDone, FinishReason: FinishReasonCancelled}
				return
			}

			req := provider.CompletionRequest{
				Model:       a.config.Model,
				Messages:    allMessages,
				Tools:       a.tools.ToProviderTools(),
				Temperature: a.config.Temperature,
				MaxTokens:   a.config.MaxTokens,
				Stream:      true,
			}

			events, err := a.config.Provider.Stream(ctx, req)
			if err != nil {
				select {
				case out <- StreamEvent{Type: StreamEventError, Error: err}:
				case <-ctx.Done():
				}
				return
			}

			var content string
			var toolCalls []provider.ToolCall

			for event := range events {
				if event.Type == provider.StreamEventError {
					select {
					case out <- StreamEvent{Type: StreamEventError, Error: event.Error}:
					case <-ctx.Done():
					}
					return
				}

				if event.Delta != "" {
					content += event.Delta
					select {
					case out <- StreamEvent{Type: StreamEventContentDelta, Delta: event.Delta, Step: step}:
					case <-ctx.Done():
						return
					}
				}

				if event.ToolCall != nil {
					toolCalls = append(toolCalls, *event.ToolCall)
					select {
					case out <- StreamEvent{Type: StreamEventToolCallEnd, ToolCall: event.ToolCall, Step: step}:
					case <-ctx.Done():
						return
					}
				}

				if event.Type == provider.StreamEventDone {
					break
				}
			}

			select {
			case out <- StreamEvent{Type: StreamEventStepEnd, Step: step}:
			case <-ctx.Done():
				return
			}

			if len(toolCalls) == 0 {
				select {
				case out <- StreamEvent{Type: StreamEventDone, FinishReason: FinishReasonStop}:
				case <-ctx.Done():
				}
				return
			}

			allMessages = append(allMessages, provider.Message{
				Role:      provider.RoleAssistant,
				Content:   content,
				ToolCalls: toolCalls,
			})

			for _, tc := range toolCalls {
				t, err := a.tools.Get(tc.Name)
				if err != nil {
					select {
					case out <- StreamEvent{
						Type: StreamEventToolResult,
						ToolResult: &ToolResultEvent{
							Name:  tc.Name,
							Input: tc.Arguments,
							Error: err,
						},
						Step: step,
					}:
					case <-ctx.Done():
						return
					}
					continue
				}

				toolResult, err := t.Execute(ctx, tc.Arguments)
				select {
				case out <- StreamEvent{
					Type: StreamEventToolResult,
					ToolResult: &ToolResultEvent{
						Name:   tc.Name,
						Input:  tc.Arguments,
						Output: toolResult,
						Error:  err,
					},
					Step: step,
				}:
				case <-ctx.Done():
					return
				}

				resultContent := fmt.Sprintf("%v", toolResult)
				if err != nil {
					resultContent = fmt.Sprintf("error: %v", err)
				}
				allMessages = append(allMessages, provider.Message{
					Role:       provider.RoleTool,
					ToolCallID: tc.ID,
					Content:    resultContent,
				})
			}
		}

		select {
		case out <- StreamEvent{Type: StreamEventDone, FinishReason: FinishReasonMaxSteps}:
		case <-ctx.Done():
		}
	}()

	return out, nil
}

func (a *agent) AddTool(t tool.Tool) error {
	return a.tools.Register(t)
}

func (a *agent) AddSkill(skill Skill) error {
	a.skills[skill.Name()] = skill
	for _, t := range skill.Tools() {
		if err := a.tools.Register(t); err != nil {
			return err
		}
	}
	return nil
}
