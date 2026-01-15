package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primary   = lipgloss.Color("99")  // purple
	secondary = lipgloss.Color("240") // gray
	accent    = lipgloss.Color("86")  // green
	danger    = lipgloss.Color("196") // red

	// App container
	appStyle = lipgloss.NewStyle().
			Padding(1, 2)

	mutedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	// Borders
	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(secondary)

	// Title
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primary).
			Padding(0, 1)

	// List items
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	cmdPreviewStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	// Output pane
	outputTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(secondary)

	outputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	errorStyle = lipgloss.NewStyle().
			Foreground(danger)

	// Help bar
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true)

	// Form
	labelStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(secondary).
			Padding(0, 1)

	focusedInputStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(primary).
				Padding(0, 1)

	// Status messages
	successStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)
)
