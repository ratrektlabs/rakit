package main

import (
	"fmt"
	"strings"
)

// msgRole distinguishes conversation message types.
type msgRole int

const (
	roleUser      msgRole = iota
	roleAssistant
	roleSystem
	roleToolStart
	roleToolEnd
)

// Message is a single entry in the conversation.
type Message struct {
	Role     msgRole
	Content  string
	ToolName string
}

// renderMessages builds the full conversation string for the viewport.
func renderMessages(messages []Message, width int) string {
	if len(messages) == 0 {
		return dimStyle.Render("\n  No messages yet. Type something or use /help for commands.")
	}

	var b strings.Builder
	for i, msg := range messages {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderMessage(msg, width))
	}
	return b.String()
}

func renderMessage(msg Message, width int) string {
	switch msg.Role {
	case roleUser:
		return fmt.Sprintf("\n%s %s",
			userLabelStyle.Render("You:"),
			msg.Content,
		)

	case roleAssistant:
		content := msg.Content
		if content == "" {
			return assistantLabelStyle.Render("Assistant:") + dimStyle.Render(" ...")
		}
		return fmt.Sprintf("\n%s %s",
			assistantLabelStyle.Render("Assistant:"),
			assistantMsgStyle.Render(content),
		)

	case roleSystem:
		return "\n" + systemMsgStyle.Render(msg.Content)

	case roleToolStart:
		return toolMsgStyle.Render(fmt.Sprintf("  ⚙ Running %s...", msg.ToolName))

	case roleToolEnd:
		result := msg.Content
		if len(result) > 150 {
			result = result[:150] + "..."
		}
		if result != "" {
			return toolDoneStyle.Render(fmt.Sprintf("  ✓ Done") + " " + dimStyle.Render(result))
		}
		return toolDoneStyle.Render("  ✓ Done")

	default:
		return msg.Content
	}
}
