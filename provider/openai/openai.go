package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ratrektlabs/rl-agent/provider"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4o-mini"
)

type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
	MaxRetries int
	RetryDelay time.Duration
}

type Client struct {
	config     Config
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string, opts ...func(*Config)) *Client {
	cfg := Config{
		APIKey:     apiKey,
		BaseURL:    defaultBaseURL,
		Model:      defaultModel,
		MaxRetries: 3,
		RetryDelay: time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		config:     cfg,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func WithBaseURL(baseURL string) func(*Config) {
	return func(cfg *Config) {
		cfg.BaseURL = baseURL
	}
}

func WithModel(model string) func(*Config) {
	return func(cfg *Config) {
		cfg.Model = model
	}
}

func WithHTTPClient(client *http.Client) func(*Config) {
	return func(cfg *Config) {
		cfg.HTTPClient = client
	}
}

func (c *Client) Name() string {
	return "openai"
}

func (c *Client) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{
		SupportsStreaming:   true,
		SupportsToolCalling: true,
		SupportsVision:      true,
	}
}

func (c *Client) Complete(ctx context.Context, req provider.CompletionRequest) (*provider.CompletionResponse, error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	req.Stream = false
	openaiReq := c.toOpenAIRequest(req)
	body, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, provider.WrapError(c.Name(), err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, provider.WrapError(c.Name(), err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.WrapError(c.Name(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}
	var openaiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, provider.NewProviderError(c.Name(), resp.StatusCode, "failed to decode response", err)
	}
	if len(openaiResp.Choices) == 0 {
		return nil, provider.NewProviderError(c.Name(), resp.StatusCode, "no choices in response", provider.ErrNoResponse)
	}
	return c.toCompletionResponse(&openaiResp), nil
}

func (c *Client) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	req.Stream = true
	openaiReq := c.toOpenAIRequest(req)
	body, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, provider.WrapError(c.Name(), err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, provider.WrapError(c.Name(), err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.WrapError(c.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, c.handleErrorResponse(resp)
	}
	return c.parseSSEStream(ctx, resp.Body), nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp openAIErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		pe := provider.NewProviderError(c.Name(), resp.StatusCode, errResp.Error.Message, nil)
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := c.parseRetryAfter(resp.Header.Get("Retry-After"))
			return provider.NewRateLimitError(c.Name(), retryAfter)
		}
		return pe
	}
	return provider.NewProviderError(c.Name(), resp.StatusCode, string(body), nil)
}

func (c *Client) parseRetryAfter(value string) time.Duration {
	if value == "" {
		return c.config.RetryDelay
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		return time.Until(t)
	}
	return c.config.RetryDelay
}

func (c *Client) toOpenAIRequest(req provider.CompletionRequest) openAIRequest {
	messages := make([]openAIMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = openAIMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]openAIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				toolCalls[j] = openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIFunctionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				}
			}
			messages[i].ToolCalls = toolCalls
		}
		if msg.ToolCallID != "" {
			messages[i].ToolCallID = msg.ToolCallID
		}
		if msg.Name != "" {
			messages[i].Name = msg.Name
		}
	}
	tools := make([]openAITool, len(req.Tools))
	for i, tool := range req.Tools {
		tools[i] = openAITool{
			Type: tool.Type,
			Function: openAIToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}
	oaiReq := openAIRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   req.Stream,
	}
	if len(tools) > 0 {
		oaiReq.Tools = tools
	}
	if req.Temperature != nil {
		oaiReq.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		oaiReq.MaxTokens = req.MaxTokens
	}
	return oaiReq
}

func (c *Client) toCompletionResponse(resp *openAIResponse) *provider.CompletionResponse {
	result := &provider.CompletionResponse{
		ID:      resp.ID,
		Model:   resp.Model,
		Content: "",
		Usage: provider.UsageStats{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		result.Content = choice.Message.Content
		result.FinishReason = choice.FinishReason
		if len(choice.Message.ToolCalls) > 0 {
			toolCalls := make([]provider.ToolCall, len(choice.Message.ToolCalls))
			for i, tc := range choice.Message.ToolCalls {
				var args map[string]any
				if tc.Function.Arguments != "" {
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}
				toolCalls[i] = provider.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				}
			}
			result.ToolCalls = toolCalls
		}
	}
	return result
}

func (c *Client) parseSSEStream(ctx context.Context, reader io.Reader) <-chan provider.StreamEvent {
	builder := provider.NewStreamBuilder(ctx, 64)
	go func() {
		defer builder.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				builder.EmitDone("stop")
				return
			}
			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				builder.EmitError(provider.NewProviderError(c.Name(), 0, "failed to parse stream chunk", err))
				return
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.Delta.Content != "" {
				if !builder.EmitDelta(choice.Delta.Content) {
					return
				}
			}
			if len(choice.Delta.ToolCalls) > 0 {
				for _, tc := range choice.Delta.ToolCalls {
					toolCall := &provider.ToolCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
					}
					if tc.Function.Arguments != "" {
						var args map[string]any
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
							toolCall.Arguments = args
						}
					}
					if !builder.EmitToolCall(toolCall) {
						return
					}
				}
			}
			if choice.FinishReason != "" {
				builder.EmitDone(choice.FinishReason)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			builder.EmitError(provider.WrapError(c.Name(), err))
		}
	}()
	return builder.Channel()
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []openAIChoice   `json:"choices"`
	Usage   openAIUsageStats `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIErrorResponse struct {
	Error openAIErrorDetail `json:"error"`
}

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Role      string                      `json:"role,omitempty"`
	Content   string                      `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCallDelta `json:"tool_calls,omitempty"`
}

type openAIStreamToolCallDelta struct {
	Index    int                     `json:"index"`
	ID       string                  `json:"id,omitempty"`
	Type     string                  `json:"type,omitempty"`
	Function openAIFunctionCallDelta `json:"function,omitempty"`
}

type openAIFunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

var _ provider.Provider = (*Client)(nil)
