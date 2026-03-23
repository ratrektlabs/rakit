package zai

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

type ZaiProvider struct {
	client  *http.Client
	apiKey  string
	baseURL string
	model   string
}

func NewProvider(config provider.ProviderConfig) *ZaiProvider {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.zukijourney.com/v1"
	}

	model := config.Model
	if model == "" {
		model = "zai-1"
	}

	return &ZaiProvider{
		client:  &http.Client{},
		apiKey:  config.APIKey,
		baseURL: baseURL,
		model:   model,
	}
}

func (p *ZaiProvider) Name() string {
	return "zai"
}

func (p *ZaiProvider) SupportsStreaming() bool {
	return true
}

func (p *ZaiProvider) SupportsTools() bool {
	return true
}

type zaiRequest struct {
	Model       string       `json:"model"`
	Messages    []zaiMessage `json:"messages"`
	Temperature *float32     `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	TopP        *float32     `json:"top_p,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
	Tools       []zaiTool    `json:"tools,omitempty"`
}

type zaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCalls  []zaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type zaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function zaiFunctionCall `json:"function"`
}

type zaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type zaiTool struct {
	Type     string          `json:"type"`
	Function *zaiFunctionDef `json:"function,omitempty"`
}

type zaiFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type zaiResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []zaiChoice `json:"choices"`
	Usage   zaiUsage    `json:"usage"`
	Error   *zaiError   `json:"error,omitempty"`
}

type zaiChoice struct {
	Index        int         `json:"index"`
	Message      *zaiMessage `json:"message,omitempty"`
	Delta        *zaiDelta   `json:"delta,omitempty"`
	FinishReason string      `json:"finish_reason"`
}

type zaiDelta struct {
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []zaiToolCall `json:"tool_calls,omitempty"`
}

type zaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type zaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type apiError struct {
	Error zaiError `json:"error"`
}

func (p *ZaiProvider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	zaiReq := p.buildRequest(req, false)

	body, err := json.Marshal(zaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("api error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var zaiResp zaiResponse
	if err := json.Unmarshal(respBody, &zaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return p.convertResponse(&zaiResp), nil
}

func (p *ZaiProvider) Stream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	zaiReq := p.buildRequest(req, true)

	body, err := json.Marshal(zaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("api error: %s", apiErr.Error.Message)
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

func (p *ZaiProvider) processStream(reader io.Reader, eventChan chan<- provider.StreamEvent) {
	scanner := bufio.NewScanner(reader)
	var data strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if data.Len() > 0 {
				p.processSSEEvent(data.String(), eventChan)
				data.Reset()
			}
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}

	if data.Len() > 0 {
		p.processSSEEvent(data.String(), eventChan)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: err}
	}
}

func (p *ZaiProvider) processSSEEvent(data string, eventChan chan<- provider.StreamEvent) {
	if data == "[DONE]" {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventDone}
		return
	}

	var zaiResp zaiResponse
	if err := json.Unmarshal([]byte(data), &zaiResp); err != nil {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: fmt.Errorf("unmarshal stream event: %w", err)}
		return
	}

	if zaiResp.Error != nil {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: fmt.Errorf("api error: %s", zaiResp.Error.Message)}
		return
	}

	if len(zaiResp.Choices) == 0 {
		return
	}

	choice := zaiResp.Choices[0]
	if choice.Delta == nil {
		return
	}

	if choice.Delta.Content != "" {
		eventChan <- provider.StreamEvent{
			Type:  provider.StreamEventContentDelta,
			Delta: choice.Delta.Content,
		}
	}

	for _, tc := range choice.Delta.ToolCalls {
		eventChan <- provider.StreamEvent{
			Type: provider.StreamEventToolCall,
			ToolCall: &provider.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: provider.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: json.RawMessage(tc.Function.Arguments),
				},
			},
		}
	}
}

func (p *ZaiProvider) buildRequest(req *provider.CompletionRequest, stream bool) *zaiRequest {
	zaiReq := &zaiRequest{
		Model:    p.getModel(req),
		Messages: p.convertMessages(req.Messages),
		Stream:   stream,
	}

	if req.Temperature != nil {
		temp := float32(*req.Temperature)
		zaiReq.Temperature = &temp
	}
	if req.MaxTokens > 0 {
		zaiReq.MaxTokens = req.MaxTokens
	}
	if req.TopP != nil {
		topP := float32(*req.TopP)
		zaiReq.TopP = &topP
	}
	if len(req.Stop) > 0 {
		zaiReq.Stop = req.Stop
	}
	if len(req.Tools) > 0 {
		zaiReq.Tools = p.convertTools(req.Tools)
	}

	return zaiReq
}

func (p *ZaiProvider) getModel(req *provider.CompletionRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return p.model
}

func (p *ZaiProvider) convertMessages(messages []provider.Message) []zaiMessage {
	result := make([]zaiMessage, len(messages))
	for i, msg := range messages {
		result[i] = zaiMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}

		if len(msg.ToolCalls) > 0 {
			result[i].ToolCalls = make([]zaiToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				result[i].ToolCalls[j] = zaiToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: zaiFunctionCall{
						Name:      tc.Function.Name,
						Arguments: string(tc.Function.Arguments),
					},
				}
			}
		}
	}
	return result
}

func (p *ZaiProvider) convertTools(tools []provider.ToolDefinition) []zaiTool {
	result := make([]zaiTool, len(tools))
	for i, tool := range tools {
		result[i] = zaiTool{
			Type: tool.Type,
			Function: &zaiFunctionDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}
	return result
}

func (p *ZaiProvider) convertResponse(resp *zaiResponse) *provider.CompletionResponse {
	choices := make([]provider.Choice, len(resp.Choices))
	for i, choice := range resp.Choices {
		choices[i] = provider.Choice{
			Index:        choice.Index,
			FinishReason: choice.FinishReason,
		}

		if choice.Message != nil {
			choices[i].Message = provider.Message{
				Role:    provider.Role(choice.Message.Role),
				Content: choice.Message.Content,
				Name:    choice.Message.Name,
			}

			if len(choice.Message.ToolCalls) > 0 {
				choices[i].Message.ToolCalls = make([]provider.ToolCall, len(choice.Message.ToolCalls))
				for j, tc := range choice.Message.ToolCalls {
					choices[i].Message.ToolCalls[j] = provider.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: provider.FunctionCall{
							Name:      tc.Function.Name,
							Arguments: json.RawMessage(tc.Function.Arguments),
						},
					}
				}
			}
		}
	}

	return &provider.CompletionResponse{
		ID:      resp.ID,
		Model:   resp.Model,
		Choices: choices,
		Usage: provider.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}
