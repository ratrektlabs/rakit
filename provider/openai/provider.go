package openai

import (
	"context"
	"encoding/json"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/ratrektlabs/rl-agent/provider"
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

func (p *Provider) Name() string  { return "openai" }
func (p *Provider) Model() string { return p.model }

func (p *Provider) Models() []string {
	return []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano"}
}

func (p *Provider) Stream(ctx context.Context, req *provider.Request) (<-chan provider.Event, error) {
	events := make(chan provider.Event, 100)

	params := openai.ChatCompletionNewParams{
		Model: p.model,
	}

	for _, msg := range req.Messages {
		params.Messages = append(params.Messages, toOpenAIMessage(msg)...)
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
	params := openai.ChatCompletionNewParams{
		Model: p.model,
	}

	for _, msg := range req.Messages {
		params.Messages = append(params.Messages, toOpenAIMessage(msg)...)
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

func toOpenAIMessage(msg provider.Message) []openai.ChatCompletionMessageParamUnion {
	var params []openai.ChatCompletionMessageParamUnion

	switch msg.Role {
	case "system":
		params = append(params, openai.SystemMessage(msg.Content))
	case "user":
		params = append(params, openai.UserMessage(msg.Content))
	case "assistant":
		params = append(params, openai.AssistantMessage(msg.Content))
		for _, tc := range msg.ToolCalls {
			params = append(params, openai.ToolMessage(tc.Arguments, tc.ID))
		}
	case "tool":
		params = append(params, openai.ToolMessage(msg.Content, ""))
	}

	return params
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
