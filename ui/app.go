package ui

import (
	"fmt"
	"strings"

	"cmdbox/db"
	"cmdbox/model"
	"cmdbox/runner"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

type mode int

const (
	modeNormal mode = iota
	modeAdd
	modeEdit
	modeDelete
	modeParam
)

type App struct {
	db       *db.DB
	commands []model.Command
	filtered []model.Command

	// UI state
	mode     mode
	cursor   int
	width    int
	height   int
	err      string
	status   string

	// Search
	searchInput textinput.Model

	// Output
	output      viewport.Model
	outputLines []string
	running     bool
	outputChan  chan runner.OutputMsg

	// Form (add/edit)
	formInputs  []textinput.Model
	formFocus   int
	editingCmd  *model.Command

	// Param input
	paramNames   []string
	paramValues  map[string]string
	paramIndex   int
	paramInput   textinput.Model
	pendingCmd   *model.Command
}

func NewApp(database *db.DB) (*App, error) {
	commands, err := database.List()
	if err != nil {
		return nil, err
	}

	search := textinput.New()
	search.Placeholder = "Search commands..."
	search.Focus()

	output := viewport.New(80, 10)

	app := &App{
		db:          database,
		commands:    commands,
		filtered:    commands,
		searchInput: search,
		output:      output,
		paramValues: make(map[string]string),
	}

	return app, nil
}

func (a *App) Init() tea.Cmd {
	return textinput.Blink
}

type outputMsg runner.OutputMsg

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width - 4  // account for app padding
		a.height = msg.Height - 2 // account for app padding
		a.output.Width = a.width - 4
		a.output.Height = a.height / 3
		return a, nil

	case outputMsg:
		if msg.Done {
			a.running = false
			a.outputChan = nil
			if msg.ErrMsg != "" {
				a.outputLines = append(a.outputLines, errorStyle.Render("Error: "+msg.ErrMsg))
			}
			a.output.SetContent(strings.Join(a.outputLines, "\n"))
			a.output.GotoBottom()
			return a, nil
		}
		line := msg.Line
		if msg.IsErr {
			line = errorStyle.Render(line)
		}
		a.outputLines = append(a.outputLines, line)
		a.output.SetContent(strings.Join(a.outputLines, "\n"))
		a.output.GotoBottom()
		// Keep reading from channel
		return a, waitForOutput(a.outputChan)

	case tea.KeyMsg:
		a.err = ""
		a.status = ""

		switch a.mode {
		case modeNormal:
			return a.updateNormal(msg)
		case modeAdd, modeEdit:
			return a.updateForm(msg)
		case modeDelete:
			return a.updateDelete(msg)
		case modeParam:
			return a.updateParam(msg)
		}
	}

	return a, nil
}

func (a *App) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return a, tea.Quit

	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}

	case "down", "j":
		if a.cursor < len(a.filtered)-1 {
			a.cursor++
		}

	case "enter":
		if len(a.filtered) > 0 {
			return a.runSelectedCommand()
		}

	case "a":
		a.mode = modeAdd
		a.initForm(nil)
		return a, nil

	case "e":
		if len(a.filtered) > 0 {
			a.mode = modeEdit
			cmd := a.filtered[a.cursor]
			a.editingCmd = &cmd
			a.initForm(&cmd)
		}
		return a, nil

	case "d":
		if len(a.filtered) > 0 {
			a.mode = modeDelete
		}
		return a, nil

	case "esc":
		a.searchInput.SetValue("")
		a.filterCommands()

	default:
		var cmd tea.Cmd
		a.searchInput, cmd = a.searchInput.Update(msg)
		a.filterCommands()
		return a, cmd
	}

	return a, nil
}

func (a *App) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit

	case "esc":
		a.mode = modeNormal
		a.searchInput.Focus()
		return a, nil

	case "tab", "down":
		a.formFocus = (a.formFocus + 1) % len(a.formInputs)
		return a, a.focusFormInput()

	case "shift+tab", "up":
		a.formFocus--
		if a.formFocus < 0 {
			a.formFocus = len(a.formInputs) - 1
		}
		return a, a.focusFormInput()

	case "enter":
		return a.submitForm()

	default:
		var cmd tea.Cmd
		a.formInputs[a.formFocus], cmd = a.formInputs[a.formFocus].Update(msg)
		return a, cmd
	}
}

func (a *App) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(a.filtered) > 0 {
			cmd := a.filtered[a.cursor]
			if err := a.db.Delete(cmd.ID); err != nil {
				a.err = err.Error()
			} else {
				a.status = "Deleted!"
				a.refreshCommands()
				if a.cursor >= len(a.filtered) && a.cursor > 0 {
					a.cursor--
				}
			}
		}
		a.mode = modeNormal
		return a, nil

	case "n", "N", "esc":
		a.mode = modeNormal
		return a, nil
	}

	return a, nil
}

func (a *App) updateParam(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit

	case "esc":
		a.mode = modeNormal
		a.searchInput.Focus()
		return a, nil

	case "enter":
		// Save current param value
		a.paramValues[a.paramNames[a.paramIndex]] = a.paramInput.Value()
		a.paramIndex++

		if a.paramIndex >= len(a.paramNames) {
			// All params collected, run the command
			return a.executeCommand()
		}

		// Next param
		a.paramInput.SetValue("")
		a.paramInput.Placeholder = a.paramNames[a.paramIndex]
		return a, nil

	default:
		var cmd tea.Cmd
		a.paramInput, cmd = a.paramInput.Update(msg)
		return a, cmd
	}
}

func (a *App) runSelectedCommand() (tea.Model, tea.Cmd) {
	cmd := a.filtered[a.cursor]
	params := runner.ExtractParams(cmd.Cmd)

	if len(params) > 0 {
		a.mode = modeParam
		a.paramNames = params
		a.paramValues = make(map[string]string)
		a.paramIndex = 0
		a.pendingCmd = &cmd
		a.paramInput = textinput.New()
		a.paramInput.Placeholder = params[0]
		a.paramInput.Focus()
		return a, nil
	}

	a.pendingCmd = &cmd
	return a.executeCommand()
}

func (a *App) executeCommand() (tea.Model, tea.Cmd) {
	cmd := a.pendingCmd
	finalCmd := runner.SubstituteParams(cmd.Cmd, a.paramValues)

	a.db.UpdateLastUsed(cmd.ID)
	a.running = true
	a.outputLines = []string{cmdPreviewStyle.Render("$ " + finalCmd), ""}
	a.output.SetContent(strings.Join(a.outputLines, "\n"))

	a.mode = modeNormal
	a.searchInput.Focus()

	// Start command in goroutine
	a.outputChan = make(chan runner.OutputMsg)
	go runner.Run(finalCmd, a.outputChan)

	return a, waitForOutput(a.outputChan)
}

func waitForOutput(ch chan runner.OutputMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return outputMsg{Done: true}
		}
		return outputMsg(msg)
	}
}

func (a *App) initForm(cmd *model.Command) {
	a.formInputs = make([]textinput.Model, 3)

	nameInput := textinput.New()
	nameInput.Placeholder = "Name (e.g., deploy prod)"
	nameInput.Focus()

	cmdInput := textinput.New()
	cmdInput.Placeholder = "Command (use {{param}} for dynamic values)"

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)"

	if cmd != nil {
		nameInput.SetValue(cmd.Name)
		cmdInput.SetValue(cmd.Cmd)
		descInput.SetValue(cmd.Description)
	}

	a.formInputs[0] = nameInput
	a.formInputs[1] = cmdInput
	a.formInputs[2] = descInput
	a.formFocus = 0
}

func (a *App) focusFormInput() tea.Cmd {
	for i := range a.formInputs {
		a.formInputs[i].Blur()
	}
	return a.formInputs[a.formFocus].Focus()
}

func (a *App) submitForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(a.formInputs[0].Value())
	cmd := strings.TrimSpace(a.formInputs[1].Value())
	desc := strings.TrimSpace(a.formInputs[2].Value())

	if name == "" || cmd == "" {
		a.err = "Name and command are required"
		return a, nil
	}

	excludeID := int64(0)
	if a.editingCmd != nil {
		excludeID = a.editingCmd.ID
	}

	dup, err := a.db.IsDuplicate(cmd, excludeID)
	if err != nil {
		a.err = err.Error()
		return a, nil
	}
	if dup {
		a.err = "A command with this exact command already exists"
		return a, nil
	}

	if a.mode == modeAdd {
		_, err = a.db.Add(name, cmd, desc)
		if err != nil {
			a.err = err.Error()
			return a, nil
		}
		a.status = "Added!"
	} else {
		err = a.db.Update(a.editingCmd.ID, name, cmd, desc)
		if err != nil {
			a.err = err.Error()
			return a, nil
		}
		a.status = "Updated!"
	}

	a.refreshCommands()
	a.mode = modeNormal
	a.searchInput.Focus()
	return a, nil
}

func (a *App) refreshCommands() {
	commands, err := a.db.List()
	if err != nil {
		a.err = err.Error()
		return
	}
	a.commands = commands
	a.filterCommands()
}

func (a *App) filterCommands() {
	query := a.searchInput.Value()
	if query == "" {
		a.filtered = a.commands
		return
	}

	// Build searchable strings
	var targets []string
	for _, c := range a.commands {
		targets = append(targets, c.Name+" "+c.Cmd)
	}

	matches := fuzzy.Find(query, targets)
	a.filtered = make([]model.Command, len(matches))
	for i, m := range matches {
		a.filtered[i] = a.commands[m.Index]
	}

	if a.cursor >= len(a.filtered) {
		a.cursor = max(0, len(a.filtered)-1)
	}
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render("cmdbox")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Search bar
	searchBox := a.searchInput.View()
	b.WriteString(searchBox)
	b.WriteString("\n\n")

	// Command list
	listHeight := a.height - a.output.Height - 10
	if listHeight < 3 {
		listHeight = 3
	}

	if a.mode == modeAdd || a.mode == modeEdit {
		b.WriteString(a.renderForm())
	} else {
		b.WriteString(a.renderList(listHeight))
	}

	// Delete confirmation
	if a.mode == modeDelete && len(a.filtered) > 0 {
		cmd := a.filtered[a.cursor]
		b.WriteString("\n")
		b.WriteString(warningStyle.Render(fmt.Sprintf("Delete '%s'? (y/n)", cmd.Name)))
		b.WriteString("\n")
	}

	// Param input
	if a.mode == modeParam {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render(fmt.Sprintf("Enter value for {{%s}}: ", a.paramNames[a.paramIndex])))
		b.WriteString(a.paramInput.View())
		b.WriteString("\n")
	}

	// Output pane
	b.WriteString("\n")
	outputTitle := outputTitleStyle.Render("OUTPUT")
	b.WriteString(outputTitle)
	b.WriteString("\n")

	outputBox := borderStyle.Width(a.width - 4).Render(a.output.View())
	b.WriteString(outputBox)
	b.WriteString("\n")

	// Status/error
	if a.err != "" {
		b.WriteString(errorStyle.Render("Error: " + a.err))
		b.WriteString("\n")
	}
	if a.status != "" {
		b.WriteString(successStyle.Render(a.status))
		b.WriteString("\n")
	}

	// Help bar
	b.WriteString(a.renderHelp())

	return appStyle.Render(b.String())
}

func (a *App) renderList(height int) string {
	if len(a.filtered) == 0 {
		return mutedStyle.Render("No commands found. Press 'a' to add one.\n")
	}

	var lines []string
	start := 0
	if a.cursor >= height {
		start = a.cursor - height + 1
	}

	end := start + height
	if end > len(a.filtered) {
		end = len(a.filtered)
	}

	for i := start; i < end; i++ {
		cmd := a.filtered[i]
		prefix := "  "
		style := normalStyle
		if i == a.cursor {
			prefix = "▸ "
			style = selectedStyle
		}

		name := style.Render(prefix + cmd.Name)
		preview := cmdPreviewStyle.Render("  " + truncate(cmd.Cmd, a.width-10))
		lines = append(lines, name, preview)
	}

	return strings.Join(lines, "\n") + "\n"
}

func (a *App) renderForm() string {
	var b strings.Builder

	title := "Add Command"
	if a.mode == modeEdit {
		title = "Edit Command"
	}
	b.WriteString(labelStyle.Render(title))
	b.WriteString("\n\n")

	labels := []string{"Name", "Command", "Description"}
	for i, input := range a.formInputs {
		b.WriteString(labelStyle.Render(labels[i] + ": "))
		style := inputStyle
		if i == a.formFocus {
			style = focusedInputStyle
		}
		b.WriteString(style.Width(a.width - 20).Render(input.View()))
		b.WriteString("\n\n")
	}

	b.WriteString(helpStyle.Render("tab: next field • enter: save • esc: cancel"))
	b.WriteString("\n")

	return b.String()
}

func (a *App) renderHelp() string {
	if a.mode != modeNormal {
		return ""
	}

	keys := []struct{ key, desc string }{
		{"enter", "run"},
		{"a", "add"},
		{"e", "edit"},
		{"d", "delete"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, helpKeyStyle.Render(k.key)+" "+helpStyle.Render(k.desc))
	}

	return strings.Join(parts, "  ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
