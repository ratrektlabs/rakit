package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ratrektlabs/rakit/provider"
	"github.com/ratrektlabs/rakit/storage/metadata"
)

// CompactionConfig controls when and how conversation history is compacted.
type CompactionConfig struct {
	// MaxMessages triggers compaction when session history exceeds this count.
	MaxMessages int

	// KeepRecent is the number of most-recent messages preserved verbatim.
	KeepRecent int

	// SummaryRole is the role used for the generated summary message.
	SummaryRole string
}

// DefaultCompactionConfig returns sensible defaults.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		MaxMessages: 20,
		KeepRecent:  6,
		SummaryRole: "system",
	}
}

const summarizationPrompt = `Summarize the following conversation history into a concise summary that preserves:
- Key facts, decisions, and preferences the user has stated
- Important context needed for the conversation to continue naturally
- Any pending tasks or open questions

Write the summary as a neutral observer. Be specific rather than vague.`

// shouldCompact returns true if the message history exceeds configured thresholds.
func shouldCompact(msgs []metadata.Message, cfg CompactionConfig) bool {
	if len(msgs) <= cfg.KeepRecent {
		return false
	}
	return len(msgs) > cfg.MaxMessages
}

// compact performs LLM summarization on the older portion of message history.
// It splits messages into "old" (summarized) and "recent" (kept verbatim).
// Returns the replacement messages: [summary, ...recent].
func compact(ctx context.Context, p provider.Provider, msgs []metadata.Message, cfg CompactionConfig) ([]metadata.Message, error) {
	if len(msgs) <= cfg.KeepRecent {
		return msgs, nil
	}

	splitIdx := len(msgs) - cfg.KeepRecent
	oldMsgs := msgs[:splitIdx]
	recentMsgs := msgs[splitIdx:]

	// Build a transcript of old messages for the summarizer.
	var buf strings.Builder
	for _, m := range oldMsgs {
		fmt.Fprintf(&buf, "[%s]: %s\n", m.Role, m.Content)
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&buf, "  [tool-call] %s(%s) -> %s\n", tc.Name, tc.Arguments, tc.Result)
		}
	}
	transcript := buf.String()

	resp, err := p.Generate(ctx, &provider.Request{
		Model: p.Model(),
		Messages: []provider.Message{
			{Role: "system", Content: summarizationPrompt},
			{Role: "user", Content: fmt.Sprintf("Summarize this conversation:\n\n%s", transcript)},
		},
		MaxTokens:   1024,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("compaction: summarization failed: %w", err)
	}

	summaryMsg := metadata.Message{
		ID:        generateID(),
		Role:      cfg.SummaryRole,
		Content:   resp.Content,
		CreatedAt: time.Now().UnixMilli(),
	}

	result := make([]metadata.Message, 0, 1+len(recentMsgs))
	result = append(result, summaryMsg)
	result = append(result, recentMsgs...)
	return result, nil
}

// metadataToProviderMessages converts metadata messages to provider messages.
func metadataToProviderMessages(msgs []metadata.Message) []provider.Message {
	out := make([]provider.Message, len(msgs))
	for i, m := range msgs {
		pm := provider.Message{
			Role:    m.Role,
			Content: m.Content,
		}
		for _, tc := range m.ToolCalls {
			pm.ToolCalls = append(pm.ToolCalls, provider.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		out[i] = pm
	}
	return out
}

// providerToolCallsToRecords converts provider tool calls to metadata records.
func providerToolCallsToRecords(tcs []provider.ToolCall) []metadata.ToolCallRecord {
	if len(tcs) == 0 {
		return nil
	}
	records := make([]metadata.ToolCallRecord, len(tcs))
	for i, tc := range tcs {
		records[i] = metadata.ToolCallRecord{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
			Status:    "completed",
		}
	}
	return records
}
