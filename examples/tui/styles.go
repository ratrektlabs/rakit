package main

import "charm.land/lipgloss/v2"

var (
	// --- Layout ---
	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3B3B3B"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	// --- Input ---
	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFB86C"))

	// --- Messages ---
	userLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8BE9FD")).
			Bold(true)

	assistantLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#50FA7B")).
				Bold(true)

	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F8F8F2"))

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BD93F9"))

	toolMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272A4"))

	toolDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50FA7B"))

	// --- Status bar components ---
	modelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8BE9FD")).
			Bold(true)

	// --- General ---
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50FA7B"))

	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFB86C"))

	// --- Legacy (kept for commands.go compatibility) ---
	titleStyle      = lipgloss.NewStyle()
	tabStyle        = lipgloss.NewStyle()
	activeTabStyle  = lipgloss.NewStyle()
	borderStyle     = lipgloss.NewStyle()
)
