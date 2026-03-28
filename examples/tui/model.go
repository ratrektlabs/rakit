package main

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// slashSuggestions is the list of slash command suggestions for autocomplete.
var slashSuggestions = []string{
	"/help",
	"/clear",
	"/skills",
	"/skills add ",
	"/skills delete ",
	"/skills toggle ",
	"/tools",
	"/tools add ",
	"/tools delete ",
	"/sessions",
	"/sessions new",
	"/sessions switch ",
	"/provider",
	"/model ",
}

// Model is the root Bubble Tea model — a single conversation view.
type Model struct {
	client    *Client
	viewport  viewport.Model
	input     textinput.Model
	messages  []Message
	sessionID string
	streaming bool

	streamCh     <-chan tea.Msg
	cancelStream context.CancelFunc
	provider     *ProviderInfo
	width        int
	height       int
	ready        bool
}

func newModel(c *Client) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "type a message or /help for commands..."
	ti.Focus()
	ti.CharLimit = 0
	ti.SetWidth(60)
	ti.ShowSuggestions = true
	ti.SetSuggestions(slashSuggestions)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.MouseWheelEnabled = true

	return Model{
		client:   c,
		input:    ti,
		viewport: vp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.loadProviderInfo(),
	)
}

func (m Model) loadProviderInfo() tea.Cmd {
	return func() tea.Msg {
		info, err := m.client.GetProvider(context.Background())
		if err != nil {
			return nil
		}
		return cmdResultMsg{provider: info}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(msg.Height - 3)
		m.input.SetWidth(msg.Width - 4)
		m.ready = true
		m.syncViewport()
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.streaming && m.cancelStream != nil {
				m.cancelStream()
				m.streaming = false
				m.cancelStream = nil
				m.streamCh = nil
				m.appendSystem("Stream cancelled.")
				return m, nil
			}
			return m, func() tea.Msg { return tea.Quit() }
		case "enter":
			if m.streaming {
				return m, nil
			}
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				return m, nil
			}
			m.input.SetValue("")

			// Check for slash command
			if cmd, args, ok := parseSlashCommand(val); ok {
				return m, m.handleSlashCommand(cmd, args)
			}

			// Regular chat message
			m.messages = append(m.messages, Message{Role: roleUser, Content: val})
			m.syncViewport()
			return m, m.startChat(val)
		}

	// --- Stream messages ---
	case streamStartedMsg:
		m.streaming = true
		m.streamCh = msg.ch
		m.cancelStream = msg.cancel
		m.messages = append(m.messages, Message{Role: roleAssistant, Content: ""})
		m.syncViewport()
		return m, waitForStreamMsg(msg.ch)

	case streamDeltaMsg:
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == roleAssistant {
			m.messages[len(m.messages)-1].Content += msg.delta
		}
		m.syncViewport()
		return m, waitForStreamMsg(m.streamCh)

	case streamReasoningMsg:
		return m, waitForStreamMsg(m.streamCh)

	case streamToolStartMsg:
		m.messages = append(m.messages, Message{Role: roleToolStart, ToolName: msg.toolName})
		m.syncViewport()
		return m, waitForStreamMsg(m.streamCh)

	case streamToolDeltaMsg:
		return m, waitForStreamMsg(m.streamCh)

	case streamToolResultMsg:
		result := msg.output
		if len(result) > 200 {
			result = result[:200] + "..."
		}
		m.messages = append(m.messages, Message{Role: roleToolEnd, ToolName: "", Content: result})
		m.syncViewport()
		return m, waitForStreamMsg(m.streamCh)

	case streamDoneMsg:
		m.streaming = false
		if m.cancelStream != nil {
			m.cancelStream()
		}
		m.cancelStream = nil
		m.streamCh = nil
		m.syncViewport()
		return m, nil

	case streamErrorMsg:
		m.streaming = false
		if m.cancelStream != nil {
			m.cancelStream()
		}
		m.cancelStream = nil
		m.streamCh = nil
		m.appendError(msg.err)
		return m, nil

	// --- Command results ---
	case cmdResultMsg:
		if msg.err != "" {
			m.appendError(msg.err)
		} else if msg.text != "" {
			m.appendSystem(msg.text)
		}
		if msg.provider != nil {
			m.provider = msg.provider
		}
		return m, nil

	case sessionCreatedMsg:
		m.sessionID = msg.id
		m.appendSystem(fmt.Sprintf("Session created: %s", msg.id))
		return m, nil

	case sessionCreatedAndChatMsg:
		m.sessionID = msg.sessionID
		return m, startStreamCmd(m.client, msg.message, msg.sessionID)
	}

	// Route remaining messages to sub-components
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)

	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)

	return m, tea.Batch(vpCmd, tiCmd)
}

// startChat sends a user message, auto-creating a session if needed.
func (m *Model) startChat(message string) tea.Cmd {
	if m.sessionID == "" {
		return func() tea.Msg {
			sess, err := m.client.CreateSession(context.Background())
			if err != nil {
				return streamErrorMsg{err: "failed to create session: " + err.Error()}
			}
			return sessionCreatedAndChatMsg{sessionID: sess.ID, message: message}
		}
	}
	return startStreamCmd(m.client, message, m.sessionID)
}

func (m *Model) syncViewport() {
	content := renderMessages(m.messages, m.width)
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m Model) View() tea.View {
	if !m.ready {
		return tea.NewView("Initializing...")
	}

	// Status bar
	modelStr := "unknown"
	if m.provider != nil {
		modelStr = m.provider.Model
	}
	sessionStr := "no session"
	if len(m.sessionID) > 8 {
		sessionStr = m.sessionID[:8]
	} else if m.sessionID != "" {
		sessionStr = m.sessionID
	}

	var indicator string
	if m.streaming {
		indicator = spinnerStyle.Render("...")
	} else {
		indicator = " "
	}

	statusText := fmt.Sprintf(" %s %s | %s | /help for commands",
		indicator, modelStyle.Render(modelStr), dimStyle.Render("session: "+sessionStr))
	status := statusBarStyle.Render(statusText)

	// Input line
	var inputLine string
	if m.streaming {
		inputLine = dimStyle.Render("  waiting...")
	} else {
		inputLine = promptStyle.Render("> ") + m.input.View()
	}

	// Separator between viewport and status
	sep := separatorStyle.Render(strings.Repeat("─", max(m.width, 1)))

	return tea.NewView(fmt.Sprintf("%s\n%s\n%s\n%s", m.viewport.View(), sep, status, inputLine))
}
