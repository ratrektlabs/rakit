package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// --- Stream message types ---

type streamStartedMsg struct {
	ch     <-chan tea.Msg
	cancel context.CancelFunc
}
type streamDeltaMsg struct{ delta string }
type streamToolStartMsg struct {
	toolCallID string
	toolName   string
}
type streamToolDeltaMsg struct {
	toolCallID string
	delta      string
}
type streamToolResultMsg struct {
	toolCallID string
	output     string
}
type streamReasoningMsg struct{ delta string }
type streamDoneMsg struct{}
type streamErrorMsg struct{ err string }

// startStreamCmd initiates an HTTP streaming request and bridges it into
// the Bubble Tea message loop via a channel.
func startStreamCmd(client *Client, message, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		resp, err := client.Chat(ctx, message, sessionID)
		if err != nil {
			cancel()
			return streamErrorMsg{err: err.Error()}
		}

		ch := make(chan tea.Msg, 100)
		go readStreamToChannel(resp.Body, ch, cancel)

		return streamStartedMsg{ch: ch, cancel: cancel}
	}
}

// readStreamToChannel reads SSE lines from the response body, sends typed
// messages into the channel, and cleans up when done.
func readStreamToChannel(body io.ReadCloser, ch chan<- tea.Msg, cancel context.CancelFunc) {
	defer close(ch)
	defer body.Close()
	defer cancel() // ensure context is always cleaned up

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			ch <- streamDoneMsg{}
			return
		}

		msg := parseStreamLine(data)
		if msg != nil {
			ch <- msg
		}
	}
	if err := scanner.Err(); err != nil {
		// Don't report context canceled as an error — it's intentional
		if !strings.Contains(err.Error(), "context canceled") {
			ch <- streamErrorMsg{err: err.Error()}
		} else {
			ch <- streamDoneMsg{}
		}
	}
}

// parseStreamLine parses a single SSE data payload and returns the
// appropriate typed message.
func parseStreamLine(data string) tea.Msg {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil
	}

	eventType, _ := event["type"].(string)

	switch eventType {
	case "text-delta":
		if delta, ok := event["delta"].(string); ok {
			return streamDeltaMsg{delta: delta}
		}
	case "tool-input-start":
		toolCallID, _ := event["toolCallId"].(string)
		toolName, _ := event["toolName"].(string)
		return streamToolStartMsg{toolCallID: toolCallID, toolName: toolName}
	case "tool-input-delta":
		toolCallID, _ := event["toolCallId"].(string)
		delta, _ := event["delta"].(string)
		return streamToolDeltaMsg{toolCallID: toolCallID, delta: delta}
	case "tool-output-available":
		toolCallID, _ := event["toolCallId"].(string)
		output, _ := event["output"].(string)
		return streamToolResultMsg{toolCallID: toolCallID, output: output}
	case "reasoning":
		if delta, ok := event["delta"].(string); ok {
			return streamReasoningMsg{delta: delta}
		}
	case "error":
		if e, ok := event["error"].(string); ok {
			return streamErrorMsg{err: e}
		}
	}
	return nil
}

// waitForStreamMsg reads one message from the stream channel.
func waitForStreamMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return streamDoneMsg{}
		}
		msg, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return msg
	}
}
