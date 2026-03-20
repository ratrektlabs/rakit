package gemini

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

type GeminiProvider struct {
	client  *http.Client
	apiKey  string
	baseURL string
	model   string
}

func NewProvider(config provider.ProviderConfig) *GeminiProvider {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	model := config.Model
	if model == "" {
		model = "gemini-1.5-flash"
	}

	return &GeminiProvider{
		client:  &http.Client{},
		apiKey:  config.APIKey,
		baseURL: baseURL,
		model:   model,
	}
}

func (p *GeminiProvider) Name() string {
	return "gemini"
}

func (p *GeminiProvider) SupportsStreaming() bool {
	return true
}

func (p *GeminiProvider) SupportsTools() bool {
	return true
}

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContentPart      `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiTool            `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string              `json:"role"`
	Parts []geminiContentPart `json:"parts"`
}

type geminiContentPart struct {
	Text         string                  `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResp *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type geminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

type geminiFunctionDecl struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Usage      *geminiUsage      `json:"usageMetadata,omitempty"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content       geminiContent  `json:"content"`
	FinishReason  string         `json:"finishReason"`
	SafetyRatings []geminiSafety `json:"safetyRatings,omitempty"`
}

type geminiSafety struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func (p *GeminiProvider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	geminiReq := p.buildRequest(req)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	model := p.getModel(req)
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

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
		var apiErr geminiError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
			return nil, fmt.Errorf("api error: %s", apiErr.Message)
		}
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if geminiResp.Error != nil {
		return nil, fmt.Errorf("api error: %s", geminiResp.Error.Message)
	}

	return p.convertResponse(&geminiResp, model), nil
}

func (p *GeminiProvider) Stream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	geminiReq := p.buildRequest(req)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	model := p.getModel(req)
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse", p.baseURL, model, p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var apiErr geminiError
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

func (p *GeminiProvider) processStream(reader io.Reader, eventChan chan<- provider.StreamEvent) {
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

func (p *GeminiProvider) processSSEEvent(data string, eventChan chan<- provider.StreamEvent) {
	if data == "" {
		return
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal([]byte(data), &geminiResp); err != nil {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: fmt.Errorf("unmarshal stream event: %w", err)}
		return
	}

	if geminiResp.Error != nil {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventError, Error: fmt.Errorf("api error: %s", geminiResp.Error.Message)}
		return
	}

	if len(geminiResp.Candidates) == 0 {
		return
	}

	candidate := geminiResp.Candidates[0]

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			eventChan <- provider.StreamEvent{
				Type:  provider.StreamEventContentDelta,
				Delta: part.Text,
			}
		}

		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			eventChan <- provider.StreamEvent{
				Type: provider.StreamEventToolCall,
				ToolCall: &provider.ToolCall{
					ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
					Type: "function",
					Function: provider.FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: argsJSON,
					},
				},
			}
		}
	}

	if candidate.FinishReason == "STOP" || candidate.FinishReason == "MAX_TOKENS" {
		eventChan <- provider.StreamEvent{Type: provider.StreamEventDone}
	}
}

func (p *GeminiProvider) buildRequest(req *provider.CompletionRequest) *geminiRequest {
	geminiReq := &geminiRequest{
		Contents: p.convertMessages(req.Messages),
	}

	if len(req.Tools) > 0 {
		geminiReq.Tools = p.convertTools(req.Tools)
	}

	geminiReq.GenerationConfig = &geminiGenerationConfig{}
	if req.Temperature != nil {
		geminiReq.GenerationConfig.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		geminiReq.GenerationConfig.MaxOutputTokens = req.MaxTokens
	}
	if req.TopP != nil {
		geminiReq.GenerationConfig.TopP = req.TopP
	}
	if len(req.Stop) > 0 {
		geminiReq.GenerationConfig.StopSequences = req.Stop
	}

	return geminiReq
}

func (p *GeminiProvider) getModel(req *provider.CompletionRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return p.model
}

func (p *GeminiProvider) convertMessages(messages []provider.Message) []geminiContent {
	var result []geminiContent
	var systemParts []geminiContentPart

	for _, msg := range messages {
		if msg.Role == provider.RoleSystem {
			systemParts = append(systemParts, geminiContentPart{Text: msg.Content})
			continue
		}

		content := geminiContent{
			Role:  p.convertRole(msg.Role),
			Parts: []geminiContentPart{},
		}

		if msg.Content != "" {
			content.Parts = append(content.Parts, geminiContentPart{Text: msg.Content})
		}

		for _, tc := range msg.ToolCalls {
			var args map[string]interface{}
			if len(tc.Function.Arguments) > 0 {
				json.Unmarshal(tc.Function.Arguments, &args)
			}
			content.Parts = append(content.Parts, geminiContentPart{
				FunctionCall: &geminiFunctionCall{
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}

		if msg.Role == provider.RoleTool {
			var resp map[string]interface{}
			if msg.Content != "" {
				json.Unmarshal([]byte(msg.Content), &resp)
			}
			content.Parts = append(content.Parts, geminiContentPart{
				FunctionResp: &geminiFunctionResponse{
					Name:     msg.Name,
					Response: resp,
				},
			})
		}

		result = append(result, content)
	}

	return result
}

func (p *GeminiProvider) convertRole(role provider.Role) string {
	switch role {
	case provider.RoleUser:
		return "user"
	case provider.RoleAssistant:
		return "model"
	case provider.RoleTool:
		return "user"
	default:
		return string(role)
	}
}

func (p *GeminiProvider) convertTools(tools []provider.ToolDefinition) []geminiTool {
	declarations := make([]geminiFunctionDecl, len(tools))
	for i, tool := range tools {
		declarations[i] = geminiFunctionDecl{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		}
	}
	return []geminiTool{{FunctionDeclarations: declarations}}
}

func (p *GeminiProvider) convertResponse(resp *geminiResponse, model string) *provider.CompletionResponse {
	choices := make([]provider.Choice, len(resp.Candidates))
	for i, candidate := range resp.Candidates {
		choice := provider.Choice{
			Index:        i,
			FinishReason: candidate.FinishReason,
		}

		msg := provider.Message{
			Role: provider.RoleAssistant,
		}

		var contentParts []string
		var toolCalls []provider.ToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				contentParts = append(contentParts, part.Text)
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
					Type: "function",
					Function: provider.FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: argsJSON,
					},
				})
			}
		}

		msg.Content = strings.Join(contentParts, "")
		msg.ToolCalls = toolCalls
		choice.Message = msg
		choices[i] = choice
	}

	usage := provider.Usage{}
	if resp.Usage != nil {
		usage.PromptTokens = resp.Usage.PromptTokenCount
		usage.CompletionTokens = resp.Usage.CandidatesTokenCount
		usage.TotalTokens = resp.Usage.TotalTokenCount
	}

	return &provider.CompletionResponse{
		ID:      fmt.Sprintf("gemini-%d", resp.Usage.TotalTokenCount),
		Model:   model,
		Choices: choices,
		Usage:   usage,
	}
}
