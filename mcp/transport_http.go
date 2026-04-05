package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPTransport implements Transport using standard HTTP POST (Streamable HTTP).
type HTTPTransport struct {
	url      string
	headers  map[string]string
	client   *http.Client
	nextID   int
	nextIDMu sync.Mutex
}

// NewHTTPTransport creates an HTTP-based MCP transport.
func NewHTTPTransport(url string, headers map[string]string) *HTTPTransport {
	return &HTTPTransport{
		url:     url,
		headers: headers,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (t *HTTPTransport) nextRequestID() int {
	t.nextIDMu.Lock()
	defer t.nextIDMu.Unlock()
	t.nextID++
	return t.nextID
}

func (t *HTTPTransport) Send(ctx context.Context, method string, params any, result any) error {
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      t.nextRequestID(),
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("mcp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mcp: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("mcp: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mcp: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp: server returned %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("mcp: unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("mcp: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if result != nil && rpcResp.Result != nil {
		resultBytes, _ := json.Marshal(rpcResp.Result)
		if err := json.Unmarshal(resultBytes, result); err != nil {
			return fmt.Errorf("mcp: unmarshal result: %w", err)
		}
	}

	return nil
}

func (t *HTTPTransport) Notify(ctx context.Context, method string, params any) error {
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return nil
}

func (t *HTTPTransport) Close() error {
	return nil
}
