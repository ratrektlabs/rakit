package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ratrektlabs/rl-agent/provider"
)

type AnthropicProvider struct {
	client  *http.Client
	apiKey  string
	baseURL string
	model   string
}

func NewProvider(config provider.ProviderConfig) *AnthropicProvider {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}

	model := config.Model
	if model == "" {
		model = "claude-3.7-sonnet"
	}

	return &AnthropicProvider{
		client:  &http.Client{},
		apiKey:  config.APIKey,
		baseURL: baseURL,
		model:   model,
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

func (p *AnthropicProvider) SupportsStreaming() bool {
	return true
}

func (p *AnthropicProvider) SupportsTools() bool {
	return true
}

type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ID        string          `json:"id,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
	Error      *anthropicError    `json:"error,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicStreamEvent struct {
	Type         string             `json:"type"`
	Index        int                `json:"index,omitempty"`
	Delta        *anthropicDelta    `json:"delta,omitempty"`
	Message      *anthropicResponse `json:"message,omitempty"`
	ContentBlock *anthropicContent  `json:"content_block,omitempty"`
	Usage        *anthropicUsage    `json:"usage,omitempty"`
	Error        *anthropicError    `json:"error,omitempty"`
}

type anthropicDelta struct {
	Type       string          `json:"type,omitempty"`
	Text       string          `json:"text,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
	Input      json.RawMessage `json:"partial_json,omitempty"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	anthropicReq := p.buildRequest(req, false)

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr anthropicError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
			return nil, fmt.Errorf("api error: %s", apiErr.Message)
		}
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("api error: %s", anthropicResp.Error.Message)
	}

	return p.convertResponse(&anthropicResp), nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	anthropicReq := p.buildRequest(req, true)

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var apiErr anthropicError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
			return nil, fmt.Errorf("api error: %s", apiErr.Message)
		}
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	eventChan := make(chan provider.StreamEvent, 100)

	go func() {
		defer close(eventChan)
		defer resp.Body.Close()

		p.processStream(resp.Body, eventChan)
	}()

	return eventChan, nil
}

func (p *AnthropicProvider) processStream(reader io.Reader, eventChan chan<- provider.StreamEvent) {
	scanner := bufio.NewScanner(reader)
	var eventType string
	var data strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data.WriteString(strings.TrimPrefix(line, "data: "))
			continue
		}

		if line == "" && data.Len() > 0 {
			p.processSSEEvent(eventType, data.String(), eventChan)
			data.Reset()
			eventType = ""
		}
	}

	if data.Len() > 0 {
		p.processSSEEvent(eventType, data.String(), eventChan)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: err}
	}
}

func (p *AnthropicProvider) processSSEEvent(eventType, data string, eventChan chan<- provider.StreamEvent) {
	if data == "" {
		return
	}

	var streamEvent anthropicStreamEvent
	if err := json.Unmarshal([]byte(data), &streamEvent); err != nil {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: fmt.Errorf("unmarshal stream event: %w", err)}
		return
	}

	if streamEvent.Error != nil {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: fmt.Errorf("api error: %s", streamEvent.Error.Message)}
		return
	}

	switch eventType {
	case "content_block_delta":
		if streamEvent.Delta != nil && streamEvent.Delta.Text != "" {
			eventChan <- provider.StreamEvent{
				Type:  provider.StreamEventContentDelta,
				Delta: streamEvent.Delta.Text,
			}
		}

	case "content_block_start":
		if streamEvent.ContentBlock != nil && streamEvent.ContentBlock.Type == "tool_use" {
			eventChan <- provider.StreamEvent{
				Type: provider.StreamEventToolCall,
				ToolCall: &provider.ToolCall{
					ID:   streamEvent.ContentBlock.ID,
					Type: "function",
					Function: provider.FunctionCall{
						Name:      streamEvent.ContentBlock.Name,
						Arguments: streamEvent.ContentBlock.Input,
					},
				},
			}
		}

	case "message_stop":
		eventChan <- provider.StreamEvent{Type: provider.StreamEventDone}
	}
}

func (p *AnthropicProvider) buildRequest(req *provider.CompletionRequest, stream bool) *anthropicRequest {
	anthropicReq := &anthropicRequest{
		Model:     p.getModel(req),
		MaxTokens: 4096,
		Stream:    stream,
	}

	if req.MaxTokens > 0 {
		anthropicReq.MaxTokens = req.MaxTokens
	}

	messages, system := p.convertMessages(req.Messages)
	anthropicReq.Messages = messages
	anthropicReq.System = system

	if req.Temperature != nil {
		anthropicReq.Temperature = req.Temperature
	}
	if req.TopP != nil {
		anthropicReq.TopP = req.TopP
	}
	if len(req.Stop) > 0 {
		anthropicReq.StopSequences = req.Stop
	}
	if len(req.Tools) > 0 {
		anthropicReq.Tools = p.convertTools(req.Tools)
	}

	return anthropicReq
}

func (p *AnthropicProvider) getModel(req *provider.CompletionRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return p.model
}

func (p *AnthropicProvider) convertMessages(messages []provider.Message) ([]anthropicMessage, string) {
	var result []anthropicMessage
	var system string

	for _, msg := range messages {
		if msg.Role == provider.RoleSystem {
			system = msg.Content
			continue
		}

		anthropicMsg := anthropicMessage{
			Role:    p.convertRole(msg.Role),
			Content: []anthropicContent{},
		}

		if msg.Content != "" {
			anthropicMsg.Content = append(anthropicMsg.Content, anthropicContent{
				Type: "text",
				Text: msg.Content,
			})
		}

		for _, tc := range msg.ToolCalls {
			anthropicMsg.Content = append(anthropicMsg.Content, anthropicContent{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: tc.Function.Arguments,
			})
		}

		if msg.Role == provider.RoleTool {
			anthropicMsg.Content = append(anthropicMsg.Content, anthropicContent{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   json.RawMessage(msg.Content),
			})
		}

		result = append(result, anthropicMsg)
	}

	return result, system
}

func (p *AnthropicProvider) convertRole(role provider.Role) string {
	switch role {
	case provider.RoleUser:
		return "user"
	case provider.RoleAssistant:
		return "assistant"
	case provider.RoleTool:
		return "user"
	default:
		return string(role)
	}
}

func (p *AnthropicProvider) convertTools(tools []provider.ToolDefinition) []anthropicTool {
	result := make([]anthropicTool, len(tools))
	for i, tool := range tools {
		result[i] = anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		}
	}
	return result
}

func (p *AnthropicProvider) convertResponse(resp *anthropicResponse) *provider.CompletionResponse {
	msg := provider.Message{
		Role: provider.RoleAssistant,
	}

	var toolCalls []provider.ToolCall

	for _, content := range resp.Content {
		if content.Type == "text" {
			msg.Content += content.Text
		}
		if content.Type == "tool_use" {
			toolCalls = append(toolCalls, provider.ToolCall{
				ID:   content.ID,
				Type: "function",
				Function: provider.FunctionCall{
					Name:      content.Name,
					Arguments: content.Input,
				},
			})
		}
	}

	msg.ToolCalls = toolCalls

	return &provider.CompletionResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Choices: []provider.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: resp.StopReason,
		}},
		Usage: provider.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}
