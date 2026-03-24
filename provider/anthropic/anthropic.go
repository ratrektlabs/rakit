package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ratrektlabs/rl-agent/provider"
)

// Provider implements provider.Provider for Anthropic Claude API
type Provider struct {
	apiKey     string
	model      string
	httpClient *http.Client
	baseURL    string
}

// NewProvider creates a new Anthropic provider
func NewProvider(apiKey, model string) *Provider {
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}
	return &Provider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com/v1",
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "anthropic"
}

// Capabilities returns provider capabilities
func (p *Provider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{
		SupportsStreaming:      true,
		SupportsToolCalling:    true,
		SupportsVision:         true,
	}
}

// Complete sends a non-streaming completion request
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	anthropicReq := p.buildRequest(req)

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "failed to marshal request", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("anthropic-dangerous-direct-browser-access", "true")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.NewProviderError(p.Name(), resp.StatusCode, "unexpected status: "+resp.Status, nil)
	}

	var anthropicResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "failed to decode response", err)
	}

	return p.parseResponse(&anthropicResp), nil
}

// Stream sends a streaming completion request
func (p *Provider) Stream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	anthropicReq := p.buildRequest(req)
	anthropicReq.Stream = true

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "failed to marshal request", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.NewProviderError(p.Name(), 0, "request failed", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, provider.NewProviderError(p.Name(), resp.StatusCode, "unexpected status: "+resp.Status, nil)
	}

	eventChan := make(chan provider.StreamEvent, 100)

	go func() {
		defer close(eventChan)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var sseEvent sseEvent
			if err := json.Unmarshal([]byte(data), &sseEvent); err != nil {
				continue
			}

			switch sseEvent.Type {
			case "content_block_delta":
				if sseEvent.Delta != nil && sseEvent.Delta.Text != "" {
					eventChan <- provider.StreamEvent{
						Type:  provider.StreamEventContentDelta,
						Delta: sseEvent.Delta.Text,
					}
				}
			case "message_stop":
				eventChan <- provider.StreamEvent{
					Type: provider.StreamEventDone,
				}
				return
			case "error":
				eventChan <- provider.StreamEvent{
					Type:  provider.StreamEventError,
					Error: provider.NewProviderError(p.Name(), 0, sseEvent.Error.Message, nil),
				}
				return
			}
		}
	}()

	return eventChan, nil
}

func (p *Provider) buildRequest(req *provider.CompletionRequest) *anthropicRequest {
	messages := make([]anthropicMessage, 0, len(req.Messages))

	for _, msg := range req.Messages {
		messages = append(messages, anthropicMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}

	ar := &anthropicRequest{
		Model:    p.model,
		Messages: messages,
		MaxTokens: 4096,
	}

	if req.MaxTokens > 0 {
		ar.MaxTokens = req.MaxTokens
	}

	if len(req.Tools) > 0 {
		ar.Tools = make([]anthropicTool, len(req.Tools))
		for i, tool := range req.Tools {
			ar.Tools[i] = anthropicTool{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				InputSchema: tool.Function.Parameters,
			}
		}
	}

	return ar
}

func (p *Provider) parseResponse(resp *anthropicResponse) *provider.CompletionResponse {
	result := &provider.CompletionResponse{
		Content:       "",
		FinishReason:  "stop",
	}

	// Extract text content
	for _, block := range resp.Content {
		if block.Type == "text" {
			result.Content += block.Text
		}
	}

	// Extract tool calls
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			var args map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &args)
			}
			result.ToolCalls = append(result.ToolCalls, provider.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}

	if resp.Usage != nil {
		result.Usage = provider.UsageStats{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}

	return result
}

// Types for Anthropic API

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicResponse struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	Role    string           `json:"role"`
	Content []contentBlock   `json:"content"`
	Model   string           `json:"model"`
	Usage   *anthropicUsage  `json:"usage,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type sseEvent struct {
	Type  string     `json:"type"`
	Delta *deltaText `json:"delta,omitempty"`
	Error *apiError  `json:"error,omitempty"`
}

type deltaText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
