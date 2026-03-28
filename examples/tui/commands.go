package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// parseSlashCommand checks if the input is a slash command.
// Returns (command, args, true) if it is, or ("", nil, false) otherwise.
func parseSlashCommand(input string) (string, []string, bool) {
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/") {
		return "", nil, false
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil, false
	}
	return parts[0], parts[1:], true
}

// handleSlashCommand routes a slash command to its handler.
func (m *Model) handleSlashCommand(cmd string, args []string) tea.Cmd {
	switch cmd {
	case "/help":
		m.handleHelp()
		return nil
	case "/clear":
		m.messages = nil
		m.syncViewport()
		return nil
	case "/skills":
		return m.handleSkillsCommand(args)
	case "/tools":
		return m.handleToolsCommand(args)
	case "/sessions":
		return m.handleSessionsCommand(args)
	case "/provider":
		return m.handleProviderCommand()
	case "/model":
		return m.handleModelCommand(args)
	default:
		m.appendSystem(fmt.Sprintf("Unknown command: %s. Type /help for available commands.", cmd))
		return nil
	}
}

func (m *Model) handleHelp() {
	help := `Available commands:
  /help                  Show this help
  /clear                 Clear conversation
  /skills                List skills
  /skills add name|desc|instructions   Register a skill
  /skills delete <name>  Delete a skill
  /skills toggle <name>  Toggle skill on/off
  /tools                 List tools
  /tools add name|desc|handler|endpoint Save a tool
  /tools delete <name>   Delete a tool
  /sessions              List sessions
  /sessions new          Create new session
  /sessions switch <id>  Switch to session
  /provider              Show provider info
  /model <name>          Switch model`
	m.appendSystem(help)
}

// --- Skills ---

func (m *Model) handleSkillsCommand(args []string) tea.Cmd {
	if len(args) == 0 {
		return m.loadSkillsCmd()
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			m.appendSystem("Usage: /skills add name|description|instructions")
			return nil
		}
		parts := strings.SplitN(strings.Join(args[1:], " "), "|", 3)
		name := strings.TrimSpace(parts[0])
		desc := ""
		instructions := ""
		if len(parts) > 1 {
			desc = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			instructions = strings.TrimSpace(parts[2])
		}
		if name == "" {
			m.appendSystem("Skill name is required.")
			return nil
		}
		return m.registerSkillCmd(name, desc, instructions)
	case "delete":
		if len(args) < 2 {
			m.appendSystem("Usage: /skills delete <name>")
			return nil
		}
		return m.deleteSkillCmd(args[1])
	case "toggle":
		if len(args) < 2 {
			m.appendSystem("Usage: /skills toggle <name>")
			return nil
		}
		return m.toggleSkillCmd(args[1])
	default:
		m.appendSystem(fmt.Sprintf("Unknown skills subcommand: %s", args[0]))
		return nil
	}
}

func (m *Model) loadSkillsCmd() tea.Cmd {
	return func() tea.Msg {
		skills, err := m.client.ListSkills(context.Background())
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		if len(skills) == 0 {
			return cmdResultMsg{text: "No skills registered."}
		}
		var b strings.Builder
		b.WriteString("Skills:\n")
		for _, s := range skills {
			status := "enabled"
			if !s.Enabled {
				status = "disabled"
			}
			b.WriteString(fmt.Sprintf("  %-20s %-10s %s\n", s.Name, status, s.Description))
		}
		return cmdResultMsg{text: strings.TrimRight(b.String(), "\n")}
	}
}

func (m *Model) registerSkillCmd(name, desc, instructions string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.RegisterSkill(context.Background(), name, desc, instructions)
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		return cmdResultMsg{text: fmt.Sprintf("Skill '%s' registered.", name)}
	}
}

func (m *Model) deleteSkillCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.DeleteSkill(context.Background(), name)
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		return cmdResultMsg{text: fmt.Sprintf("Skill '%s' deleted.", name)}
	}
}

func (m *Model) toggleSkillCmd(name string) tea.Cmd {
	return func() tea.Msg {
		// Get current state first
		skills, err := m.client.ListSkills(context.Background())
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		for _, s := range skills {
			if s.Name == name {
				enable := !s.Enabled
				if err := m.client.ToggleSkill(context.Background(), name, enable); err != nil {
					return cmdResultMsg{err: err.Error()}
				}
				status := "enabled"
				if !enable {
					status = "disabled"
				}
				return cmdResultMsg{text: fmt.Sprintf("Skill '%s' %s.", name, status)}
			}
		}
		return cmdResultMsg{err: fmt.Sprintf("Skill '%s' not found.", name)}
	}
}

// --- Tools ---

func (m *Model) handleToolsCommand(args []string) tea.Cmd {
	if len(args) == 0 {
		return m.loadToolsCmd()
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			m.appendSystem("Usage: /tools add name|description|handler|endpoint")
			return nil
		}
		parts := strings.SplitN(strings.Join(args[1:], " "), "|", 4)
		name := strings.TrimSpace(parts[0])
		desc := ""
		handler := "http"
		endpoint := ""
		if len(parts) > 1 {
			desc = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			handler = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			endpoint = strings.TrimSpace(parts[3])
		}
		if name == "" {
			m.appendSystem("Tool name is required.")
			return nil
		}
		return m.saveToolCmd(name, desc, handler, endpoint)
	case "delete":
		if len(args) < 2 {
			m.appendSystem("Usage: /tools delete <name>")
			return nil
		}
		return m.deleteToolCmd(args[1])
	default:
		m.appendSystem(fmt.Sprintf("Unknown tools subcommand: %s", args[0]))
		return nil
	}
}

func (m *Model) loadToolsCmd() tea.Cmd {
	return func() tea.Msg {
		tools, err := m.client.ListTools(context.Background())
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		if len(tools) == 0 {
			return cmdResultMsg{text: "No tools registered."}
		}
		var b strings.Builder
		b.WriteString("Tools:\n")
		for _, t := range tools {
			b.WriteString(fmt.Sprintf("  %-20s %-12s %s\n", t.Name, t.Handler, t.Description))
		}
		return cmdResultMsg{text: strings.TrimRight(b.String(), "\n")}
	}
}

func (m *Model) saveToolCmd(name, desc, handler, endpoint string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.SaveTool(context.Background(), name, desc, handler, endpoint)
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		return cmdResultMsg{text: fmt.Sprintf("Tool '%s' saved.", name)}
	}
}

func (m *Model) deleteToolCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.DeleteTool(context.Background(), name)
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		return cmdResultMsg{text: fmt.Sprintf("Tool '%s' deleted.", name)}
	}
}

// --- Sessions ---

func (m *Model) handleSessionsCommand(args []string) tea.Cmd {
	if len(args) == 0 {
		return m.loadSessionsCmd()
	}
	switch args[0] {
	case "new":
		return m.createSessionCmd()
	case "switch":
		if len(args) < 2 {
			m.appendSystem("Usage: /sessions switch <id>")
			return nil
		}
		m.sessionID = args[1]
		m.appendSystem(fmt.Sprintf("Switched to session: %s", args[1]))
		return nil
	default:
		m.appendSystem(fmt.Sprintf("Unknown sessions subcommand: %s", args[0]))
		return nil
	}
}

func (m *Model) loadSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.client.ListSessions(context.Background())
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		if len(sessions) == 0 {
			return cmdResultMsg{text: "No sessions found."}
		}
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, s := range sessions {
			id := s.ID
			if len(id) > 12 {
				id = id[:12]
			}
			marker := " "
			if s.ID == m.sessionID {
				marker = "*"
			}
			ts := time.Unix(s.CreatedAt/1000, 0).Format("2006-01-02 15:04")
			b.WriteString(fmt.Sprintf(" %s %-14s msgs:%-4d %s\n", marker, id, s.MessageCount, ts))
		}
		return cmdResultMsg{text: strings.TrimRight(b.String(), "\n")}
	}
}

func (m *Model) createSessionCmd() tea.Cmd {
	return func() tea.Msg {
		sess, err := m.client.CreateSession(context.Background())
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		return sessionCreatedMsg{id: sess.ID}
	}
}

// --- Provider ---

func (m *Model) handleProviderCommand() tea.Cmd {
	return func() tea.Msg {
		info, err := m.client.GetProvider(context.Background())
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Provider: %s\n", info.Provider))
		b.WriteString(fmt.Sprintf("Model:    %s\n", info.Model))
		if len(info.Models) > 0 {
			b.WriteString("Available models:\n")
			for _, model := range info.Models {
				prefix := "  - "
				if model == info.Model {
					prefix = "  * "
				}
				b.WriteString(prefix + model + "\n")
			}
		}
		return cmdResultMsg{text: strings.TrimRight(b.String(), "\n"), provider: info}
	}
}

func (m *Model) handleModelCommand(args []string) tea.Cmd {
	if len(args) == 0 {
		m.appendSystem("Usage: /model <name>")
		return nil
	}
	model := strings.Join(args, " ")
	return func() tea.Msg {
		err := m.client.SetModel(context.Background(), model)
		if err != nil {
			return cmdResultMsg{err: err.Error()}
		}
		// Also refresh provider info
		info, _ := m.client.GetProvider(context.Background())
		return cmdResultMsg{
			text:     fmt.Sprintf("Model set to: %s", model),
			provider: info,
		}
	}
}

// --- Command result message ---

type cmdResultMsg struct {
	text     string
	err      string
	provider *ProviderInfo
}

// --- Session message ---

type sessionCreatedMsg struct{ id string }

type sessionCreatedAndChatMsg struct {
	sessionID string
	message   string
}

// appendSystem adds a system message to the conversation.
func (m *Model) appendSystem(text string) {
	m.messages = append(m.messages, Message{Role: roleSystem, Content: text})
	m.syncViewport()
}

// appendError adds an error as a system message.
func (m *Model) appendError(err string) {
	m.messages = append(m.messages, Message{Role: roleSystem, Content: errorStyle.Render("Error: " + err)})
	m.syncViewport()
}

// parseIntArgs is a helper for parsing numeric args.
func parseIntArgs(args []string, idx int) (int, bool) {
	if idx >= len(args) {
		return 0, false
	}
	n, err := strconv.Atoi(args[idx])
	return n, err == nil
}
