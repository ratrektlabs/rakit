package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SSETransport implements Transport using SSE (Server-Sent Events) for receiving
// and HTTP POST for sending, as defined by the MCP SSE transport spec.
type SSETransport struct {
	url         string
	headers     map[string]string
	client      *http.Client
	messageURL  string // endpoint URL received from SSE endpoint event
	nextID      int
	nextIDMu    sync.Mutex
	pending     map[int]chan *jsonRPCResponse
	pendingMu   sync.Mutex
	connected   bool
	connectedMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewSSETransport creates an SSE-based MCP transport.
// The url should be the SSE endpoint (e.g., http://server/sse).
func NewSSETransport(url string, headers map[string]string) *SSETransport {
	return &SSETransport{
		url:     url,
		headers: headers,
		client: &http.Client{
			Timeout: 0, // no timeout for SSE connections
		},
		pending: make(map[int]chan *jsonRPCResponse),
		done:    make(chan struct{}),
	}
}

// connect opens the SSE connection and starts listening for events.
func (t *SSETransport) connect(ctx context.Context) error {
	t.connectedMu.Lock()
	if t.connected {
		t.connectedMu.Unlock()
		return nil
	}
	t.connectedMu.Unlock()

	sseCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, t.url, nil)
	if err != nil {
		return fmt.Errorf("mcp sse: create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse: connect: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("mcp sse: server returned %d: %s", resp.StatusCode, string(body))
	}

	// Wait for the endpoint event with a timeout
	endpointCh := make(chan string, 1)

	go func() {
		defer resp.Body.Close()
		defer close(t.done)

		scanner := bufio.NewScanner(resp.Body)
		var eventType, eventData string

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				// Empty line = event boundary
				if eventType != "" && eventData != "" {
					t.handleSSEEvent(eventType, eventData, endpointCh)
				}
				eventType = ""
				eventData = ""
				continue
			}

			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				eventData = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Wait for the endpoint event
	select {
	case endpoint := <-endpointCh:
		// Resolve relative URL
		if strings.HasPrefix(endpoint, "/") {
			// Extract base URL
			base := t.url
			if idx := strings.Index(base, "://"); idx >= 0 {
				if slashIdx := strings.Index(base[idx+3:], "/"); slashIdx >= 0 {
					base = base[:idx+3+slashIdx]
				}
			}
			t.messageURL = base + endpoint
		} else {
			t.messageURL = endpoint
		}

		t.connectedMu.Lock()
		t.connected = true
		t.connectedMu.Unlock()

		return nil
	case <-time.After(10 * time.Second):
		cancel()
		return fmt.Errorf("mcp sse: timed out waiting for endpoint event")
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}
}

func (t *SSETransport) handleSSEEvent(eventType, data string, endpointCh chan<- string) {
	switch eventType {
	case "endpoint":
		select {
		case endpointCh <- data:
		default:
		}
	case "message":
		var rpcResp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
			return
		}
		t.pendingMu.Lock()
		if ch, ok := t.pending[rpcResp.ID]; ok {
			ch <- &rpcResp
			delete(t.pending, rpcResp.ID)
		}
		t.pendingMu.Unlock()
	}
}

func (t *SSETransport) nextRequestID() int {
	t.nextIDMu.Lock()
	defer t.nextIDMu.Unlock()
	t.nextID++
	return t.nextID
}

func (t *SSETransport) Send(ctx context.Context, method string, params any, result any) error {
	if err := t.connect(ctx); err != nil {
		return err
	}

	id := t.nextRequestID()
	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Register pending response
	respCh := make(chan *jsonRPCResponse, 1)
	t.pendingMu.Lock()
	t.pending[id] = respCh
	t.pendingMu.Unlock()

	// Send via POST to the message endpoint
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("mcp sse: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.messageURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mcp sse: create post request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	postClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := postClient.Do(httpReq)
	if err != nil {
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
		return fmt.Errorf("mcp sse: post request failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
		return fmt.Errorf("mcp sse: post returned %d", resp.StatusCode)
	}

	// Wait for response via SSE
	select {
	case rpcResp := <-respCh:
		if rpcResp.Error != nil {
			return fmt.Errorf("mcp sse: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
		}
		if result != nil && rpcResp.Result != nil {
			resultBytes, _ := json.Marshal(rpcResp.Result)
			if err := json.Unmarshal(resultBytes, result); err != nil {
				return fmt.Errorf("mcp sse: unmarshal result: %w", err)
			}
		}
		return nil
	case <-time.After(60 * time.Second):
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
		return fmt.Errorf("mcp sse: timed out waiting for response to %s", method)
	case <-ctx.Done():
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
		return ctx.Err()
	}
}

func (t *SSETransport) Notify(ctx context.Context, method string, params any) error {
	if err := t.connect(ctx); err != nil {
		return err
	}

	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.messageURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	postClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := postClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return nil
}

func (t *SSETransport) Close() error {
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}
