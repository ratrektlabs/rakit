package main

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// spinnerFrames is a simple spinner animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

	// Spinner state
	spinnerIdx   int
	inputTokens  int // approximate input chars
	outputTokens int // approximate output chars
	lastDuration time.Duration
	streamStart  time.Time
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

// tickSpinnerMsg is sent on a timer to animate the spinner.
type tickSpinnerMsg time.Time

func tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickSpinnerMsg(t)
	})
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
				m.appendSystem("Cancelled.")
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

			// Slash command
			if cmd, args, ok := parseSlashCommand(val); ok {
				return m, m.handleSlashCommand(cmd, args)
			}

			// Chat message
			m.inputTokens += len(val)
			m.messages = append(m.messages, Message{Role: roleUser, Content: val})
			m.syncViewport()
			return m, m.startChat(val)
		}

	// --- Spinner tick ---
	case tickSpinnerMsg:
		if m.streaming {
			m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
			return m, tickSpinner()
		}
		return m, nil

	// --- Stream messages ---
	case streamStartedMsg:
		m.streaming = true
		m.streamCh = msg.ch
		m.cancelStream = msg.cancel
		m.streamStart = time.Now()
		m.messages = append(m.messages, Message{Role: roleAssistant, Content: ""})
		m.syncViewport()
		return m, tea.Batch(waitForStreamMsg(msg.ch), tickSpinner())

	case streamDeltaMsg:
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == roleAssistant {
			m.messages[len(m.messages)-1].Content += msg.delta
			m.outputTokens += len(msg.delta)
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
		if len(result) > 150 {
			result = result[:150] + "..."
		}
		m.messages = append(m.messages, Message{Role: roleToolEnd, Content: result})
		m.syncViewport()
		return m, waitForStreamMsg(m.streamCh)

	case streamDoneMsg:
		m.streaming = false
		m.lastDuration = time.Since(m.streamStart)
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
		m.appendSystem(fmt.Sprintf("Session: %s", msg.id))
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

	var spinner string
	if m.streaming {
		spinner = spinnerFrames[m.spinnerIdx] + " "
	}

	// Token stats
	tokenInfo := ""
	if m.outputTokens > 0 {
		tokenInfo = fmt.Sprintf(" | out: %d", m.outputTokens)
	}
	if m.lastDuration > 0 && !m.streaming {
		tokenInfo += fmt.Sprintf(" | %.1fs", m.lastDuration.Seconds())
	}

	statusText := fmt.Sprintf(" %s%s | session: %s%s",
		spinner,
		modelStyle.Render(modelStr),
		dimStyle.Render(sessionStr),
		dimStyle.Render(tokenInfo),
	)
	status := statusBarStyle.Render(statusText)

	// Input line
	var inputLine string
	if m.streaming {
		inputLine = dimStyle.Render("  " + spinnerFrames[m.spinnerIdx] + "  thinking...")
	} else {
		inputLine = promptStyle.Render("> ") + m.input.View()
	}

	sep := separatorStyle.Render(strings.Repeat("─", max(m.width, 1)))

	return tea.NewView(fmt.Sprintf("%s\n%s\n%s", m.viewport.View(), sep, status) + "\n" + inputLine)
}
