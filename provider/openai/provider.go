package openai

import (
	"context"
	"encoding/json"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"

	"github.com/ratrektlabs/rakit/provider"
)

// Provider implements the OpenAI LLM backend.
type Provider struct {
	client openai.Client
	model  string
}

func New(model, apiKey string) *Provider {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Provider{client: client, model: model}
}

func (p *Provider) Name() string      { return "openai" }
func (p *Provider) Model() string     { return p.model }
func (p *Provider) SetModel(m string) { p.model = m }

func (p *Provider) Models() []string {
	return []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano"}
}

// buildParams constructs a ChatCompletionNewParams from a provider.Request.
// It prepends req.System (if any), converts messages preserving tool calls
// and tool_call_ids, and attaches tool schemas.
func (p *Provider) buildParams(req *provider.Request) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model: p.model,
	}

	if req.System != "" {
		params.Messages = append(params.Messages, openai.SystemMessage(req.System))
	}

	for _, msg := range req.Messages {
		params.Messages = append(params.Messages, toOpenAIMessages(msg)...)
	}

	for _, t := range req.Tools {
		params.Tools = append(params.Tools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  toOpenAIParams(t.Parameters),
		}))
	}

	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = openai.Float(req.Temperature)
	}

	return params
}

func (p *Provider) Stream(ctx context.Context, req *provider.Request) (<-chan provider.Event, error) {
	events := make(chan provider.Event, 100)

	params := p.buildParams(req)

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)
	acc := openai.ChatCompletionAccumulator{}

	go func() {
		defer close(events)

		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta
				if delta.Content != "" {
					events <- &provider.TextDeltaEvent{Delta: delta.Content}
				}
			}

			if tool, ok := acc.JustFinishedToolCall(); ok {
				events <- &provider.ToolCallEvent{
					ID:        tool.ID,
					Name:      tool.Name,
					Arguments: tool.Arguments,
				}
			}
		}

		if err := stream.Err(); err != nil {
			events <- &provider.ErrorProviderEvent{Err: err}
			return
		}

		events <- &provider.DoneProviderEvent{}
	}()

	return events, nil
}

func (p *Provider) Generate(ctx context.Context, req *provider.Request) (*provider.Response, error) {
	params := p.buildParams(req)

	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	resp := &provider.Response{}
	if len(completion.Choices) > 0 {
		resp.Content = completion.Choices[0].Message.Content
		for _, tc := range completion.Choices[0].Message.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, provider.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}

	resp.Usage = provider.Usage{
		InputTokens:  int(completion.Usage.PromptTokens),
		OutputTokens: int(completion.Usage.CompletionTokens),
	}

	return resp, nil
}

// toOpenAIMessages converts a provider.Message into one or more OpenAI message
// params. Assistant messages with tool calls are emitted as a single
// assistant message carrying a tool_calls array. Tool results are emitted
// with their tool_call_id (taken from ToolCall.ID) so OpenAI can match them
// to the original call.
func toOpenAIMessages(msg provider.Message) []openai.ChatCompletionMessageParamUnion {
	switch msg.Role {
	case "system":
		return []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(msg.Content)}
	case "user":
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(msg.Content)}
	case "assistant":
		assistant := openai.ChatCompletionAssistantMessageParam{}
		if msg.Content != "" {
			assistant.Content.OfString = param.NewOpt(msg.Content)
		}
		for _, tc := range msg.ToolCalls {
			assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				},
			})
		}
		return []openai.ChatCompletionMessageParamUnion{{OfAssistant: &assistant}}
	case "tool":
		// A tool message carries the result of a single tool_call_id.
		// The runner builds one "tool" role message per result and stores
		// the call ID inside ToolCalls[0].ID (with Name also set for
		// back-compat with the Gemini provider).
		toolCallID := ""
		if len(msg.ToolCalls) > 0 {
			toolCallID = msg.ToolCalls[0].ID
		}
		return []openai.ChatCompletionMessageParamUnion{openai.ToolMessage(msg.Content, toolCallID)}
	}
	return nil
}

func toOpenAIParams(schema any) openai.FunctionParameters {
	if schema == nil {
		return nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var params openai.FunctionParameters
	if err := json.Unmarshal(b, &params); err != nil {
		return nil
	}
	return params
}

var _ provider.Provider = (*Provider)(nil)
