package gemini

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"

	"github.com/ratrektlabs/rl-agent/provider"
)

// Provider implements the Gemini LLM backend.
type Provider struct {
	client *genai.Client
	model  string
}

func New(model, apiKey string) (*Provider, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: create client: %w", err)
	}
	return &Provider{client: client, model: model}, nil
}

func (p *Provider) Name() string  { return "gemini" }
func (p *Provider) Model() string { return p.model }

func (p *Provider) Models() []string {
	return []string{"gemini-3.1-pro-preview", "gemini-3.1-flash-lite-preview"}
}

func (p *Provider) Stream(ctx context.Context, req *provider.Request) (<-chan provider.Event, error) {
	events := make(chan provider.Event, 100)

	contents, err := toGeminiContents(req.Messages)
	if err != nil {
		return nil, err
	}

	config := &genai.GenerateContentConfig{}
	if req.MaxTokens > 0 {
		config.MaxOutputTokens = int32(req.MaxTokens)
	}
	if req.Temperature > 0 {
		t := float32(req.Temperature)
		config.Temperature = &t
	}

	for _, t := range req.Tools {
		schema, err := toGeminiSchema(t.Parameters)
		if err != nil {
			return nil, fmt.Errorf("gemini: tool %q schema: %w", t.Name, err)
		}
		config.Tools = append(config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  schema,
				},
			},
		})
	}

	iter := p.client.Models.GenerateContentStream(ctx, p.model, contents, config)

	go func() {
		defer close(events)

		for resp, err := range iter {
			if err != nil {
				events <- &provider.ErrorProviderEvent{Err: err}
				return
			}

			if resp == nil {
				continue
			}

			for _, candidate := range resp.Candidates {
				if candidate.Content == nil {
					continue
				}
				for _, part := range candidate.Content.Parts {
					switch {
					case part.Text != "":
						events <- &provider.TextDeltaEvent{Delta: part.Text}
					case part.FunctionCall != nil:
						args, _ := json.Marshal(part.FunctionCall.Args)
						events <- &provider.ToolCallEvent{
							ID:        part.FunctionCall.Name,
							Name:      part.FunctionCall.Name,
							Arguments: string(args),
						}
					}
				}
			}
		}

		events <- &provider.DoneProviderEvent{}
	}()

	return events, nil
}

func (p *Provider) Generate(ctx context.Context, req *provider.Request) (*provider.Response, error) {
	contents, err := toGeminiContents(req.Messages)
	if err != nil {
		return nil, err
	}

	config := &genai.GenerateContentConfig{}
	if req.MaxTokens > 0 {
		config.MaxOutputTokens = int32(req.MaxTokens)
	}
	if req.Temperature > 0 {
		t := float32(req.Temperature)
		config.Temperature = &t
	}

	for _, t := range req.Tools {
		schema, err := toGeminiSchema(t.Parameters)
		if err != nil {
			return nil, fmt.Errorf("gemini: tool %q schema: %w", t.Name, err)
		}
		config.Tools = append(config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  schema,
				},
			},
		})
	}

	resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, config)
	if err != nil {
		return nil, err
	}

	result := &provider.Response{}
	if resp != nil && len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.Text != "" {
				result.Content += part.Text
			}
			if part.FunctionCall != nil {
				args, _ := json.Marshal(part.FunctionCall.Args)
				result.ToolCalls = append(result.ToolCalls, provider.ToolCall{
					ID:        part.FunctionCall.Name,
					Name:      part.FunctionCall.Name,
					Arguments: string(args),
				})
			}
		}
	}

	if resp != nil && resp.UsageMetadata != nil {
		result.Usage = provider.Usage{
			InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
			OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
		}
	}

	return result, nil
}

func toGeminiContents(messages []provider.Message) ([]*genai.Content, error) {
	var contents []*genai.Content
	for _, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		var parts []*genai.Part
		if msg.Content != "" {
			parts = append(parts, &genai.Part{Text: msg.Content})
		}

		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
				args = map[string]any{}
			}
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					Name: tc.Name,
					Args: args,
				},
			})
		}

		// Handle tool results
		if msg.Role == "tool" {
			var respParts []*genai.Part
			for _, tc := range msg.ToolCalls {
				respParts = append(respParts, &genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						Name: tc.Name,
						Response: map[string]any{
							"result": tc.Arguments,
						},
					},
				})
			}
			contents = append(contents, &genai.Content{
				Role:  "function",
				Parts: respParts,
			})
			continue
		}

		if len(parts) > 0 {
			contents = append(contents, &genai.Content{
				Role:  role,
				Parts: parts,
			})
		}
	}
	return contents, nil
}

func toGeminiSchema(schema any) (*genai.Schema, error) {
	if schema == nil {
		return nil, nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var s genai.Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

var _ provider.Provider = (*Provider)(nil)
