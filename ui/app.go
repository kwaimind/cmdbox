package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"cmdbox/db"
	"cmdbox/model"
	"cmdbox/runner"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
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

type tab int

const (
	tabBash tab = iota
	tabSQL
)

type App struct {
	db       *db.DB
	commands []model.Command
	filtered []model.Command

	// SQL queries
	queries         []model.Query
	filteredQueries []model.Query

	// UI state
	mode     mode
	tab      tab
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
	formInputs   []textinput.Model
	sqlTextarea  textarea.Model
	formFocus    int
	editingCmd   *model.Command
	editingQuery *model.Query

	// Param input (inline mode)
	paramInfos  []runner.ParamInfo
	paramValues map[string]string
	paramInput  textinput.Model
	pendingCmd  *model.Command
}

func NewApp(database *db.DB) (*App, error) {
	commands, err := database.List()
	if err != nil {
		return nil, err
	}

	queries, err := database.ListQueries()
	if err != nil {
		return nil, err
	}

	search := textinput.New()
	search.Placeholder = "Search commands..."
	search.Focus()

	output := viewport.New(80, 10)

	app := &App{
		db:              database,
		commands:        commands,
		filtered:        commands,
		queries:         queries,
		filteredQueries: queries,
		searchInput:     search,
		output:          output,
		paramValues:     make(map[string]string),
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
	case "ctrl+c", "Q":
		return a, tea.Quit

	case "tab":
		a.cursor = 0
		a.searchInput.SetValue("")
		a.outputLines = []string{}
		a.output.SetContent("")
		if a.tab == tabBash {
			a.tab = tabSQL
			a.searchInput.Placeholder = "Search queries..."
		} else {
			a.tab = tabBash
			a.searchInput.Placeholder = "Search commands..."
		}
		a.filterItems()
		return a, nil

	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}

	case "down", "j":
		maxIdx := a.listLen() - 1
		if a.cursor < maxIdx {
			a.cursor++
		}

	case "enter":
		if a.tab == tabBash {
			if len(a.filtered) > 0 {
				return a.runSelectedCommand()
			}
		} else {
			// SQL tab: show query in output, don't execute
			if len(a.filteredQueries) > 0 {
				q := a.filteredQueries[a.cursor]
				a.outputLines = []string{q.SQL}
				a.output.SetContent(q.SQL)
				a.db.UpdateQueryLastUsed(q.ID)
				a.refreshQueries()
			}
		}
		return a, nil

	case "A":
		a.mode = modeAdd
		if a.tab == tabBash {
			a.initForm(nil)
		} else {
			a.initQueryForm(nil)
		}
		return a, nil

	case "E":
		if a.tab == tabBash {
			if len(a.filtered) > 0 {
				a.mode = modeEdit
				cmd := a.filtered[a.cursor]
				a.editingCmd = &cmd
				a.initForm(&cmd)
			}
		} else {
			if len(a.filteredQueries) > 0 {
				a.mode = modeEdit
				q := a.filteredQueries[a.cursor]
				a.editingQuery = &q
				a.initQueryForm(&q)
			}
		}
		return a, nil

	case "D":
		if a.listLen() > 0 {
			a.mode = modeDelete
		}
		return a, nil

	case "C":
		a.outputLines = []string{}
		a.output.SetContent("")
		return a, nil

	case "Y":
		if a.tab == tabBash {
			if len(a.filtered) > 0 {
				cmd := a.filtered[a.cursor]
				if err := clipboard.WriteAll(cmd.Cmd); err != nil {
					a.err = "Failed to copy: " + err.Error()
				} else {
					a.status = "Copied!"
				}
			}
		} else {
			if len(a.filteredQueries) > 0 {
				q := a.filteredQueries[a.cursor]
				if err := clipboard.WriteAll(q.SQL); err != nil {
					a.err = "Failed to copy: " + err.Error()
				} else {
					a.status = "Copied!"
				}
			}
		}
		return a, nil

	case "esc":
		a.searchInput.SetValue("")
		a.filterItems()

	default:
		var cmd tea.Cmd
		a.searchInput, cmd = a.searchInput.Update(msg)
		a.filterItems()
		return a, cmd
	}

	return a, nil
}

func (a *App) listLen() int {
	if a.tab == tabBash {
		return len(a.filtered)
	}
	return len(a.filteredQueries)
}

func (a *App) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// SQL form has 3 logical fields: name(0), sql(1), desc(2)
	// Bash form has 3 fields: name(0), cmd(1), desc(2)
	maxFocus := 2
	if a.tab == tabBash {
		maxFocus = len(a.formInputs) - 1
	}

	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit

	case "esc":
		a.mode = modeNormal
		a.searchInput.Focus()
		return a, nil

	case "S":
		return a.submitForm()

	case "tab", "down":
		// In SQL textarea, tab inserts tab, use ctrl+n or down to move
		if a.tab == tabSQL && a.formFocus == 1 && msg.String() == "tab" {
			var cmd tea.Cmd
			a.sqlTextarea, cmd = a.sqlTextarea.Update(msg)
			return a, cmd
		}
		a.formFocus = (a.formFocus + 1) % (maxFocus + 1)
		return a, a.focusFormInput()

	case "shift+tab", "up":
		a.formFocus--
		if a.formFocus < 0 {
			a.formFocus = maxFocus
		}
		return a, a.focusFormInput()

	case "enter":
		if a.tab == tabBash {
			return a.submitForm()
		}
		// SQL tab: enter in textarea adds newline, otherwise submit
		if a.formFocus == 1 {
			var cmd tea.Cmd
			a.sqlTextarea, cmd = a.sqlTextarea.Update(msg)
			return a, cmd
		}
		return a.submitForm()

	default:
		var cmd tea.Cmd
		if a.tab == tabSQL && a.formFocus == 1 {
			a.sqlTextarea, cmd = a.sqlTextarea.Update(msg)
		} else {
			idx := a.sqlFormInputIndex()
			a.formInputs[idx], cmd = a.formInputs[idx].Update(msg)
		}
		return a, cmd
	}
}

func (a *App) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if a.tab == tabBash {
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
		} else {
			if len(a.filteredQueries) > 0 {
				q := a.filteredQueries[a.cursor]
				if err := a.db.DeleteQuery(q.ID); err != nil {
					a.err = err.Error()
				} else {
					a.status = "Deleted!"
					a.refreshQueries()
					if a.cursor >= len(a.filteredQueries) && a.cursor > 0 {
						a.cursor--
					}
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
		// Parse inline params: key=value key2=value2
		parsed := parseInlineParams(a.paramInput.Value())
		// Validate all params present
		var missing []string
		for _, p := range a.paramInfos {
			if _, ok := parsed[p.Name]; !ok {
				missing = append(missing, p.Name)
			}
		}
		if len(missing) > 0 {
			a.err = "Missing params: " + strings.Join(missing, ", ")
			return a, nil
		}
		a.paramValues = parsed
		return a.executeCommand()

	default:
		var cmd tea.Cmd
		a.paramInput, cmd = a.paramInput.Update(msg)
		return a, cmd
	}
}

// parseInlineParams parses "key=value key2=value2" into map
func parseInlineParams(input string) map[string]string {
	result := make(map[string]string)
	parts := strings.Fields(input)
	for _, part := range parts {
		if idx := strings.Index(part, "="); idx > 0 {
			key := part[:idx]
			value := part[idx+1:]
			result[key] = value
		}
	}
	return result
}

func (a *App) runSelectedCommand() (tea.Model, tea.Cmd) {
	cmd := a.filtered[a.cursor]
	params := runner.ExtractParams(cmd.Cmd)

	if len(params) > 0 {
		a.mode = modeParam
		a.paramInfos = params
		a.paramValues = make(map[string]string)
		a.pendingCmd = &cmd

		// Load last-used values
		lastParams := make(map[string]string)
		if cmd.LastParams != "" {
			json.Unmarshal([]byte(cmd.LastParams), &lastParams)
		}

		// Build inline input: "key=value key2=value2"
		var parts []string
		for _, p := range params {
			val := ""
			if !p.Sensitive {
				val = lastParams[p.Name]
			}
			parts = append(parts, p.Name+"="+val)
		}

		a.paramInput = textinput.New()
		a.paramInput.SetValue(strings.Join(parts, " "))
		a.paramInput.Focus()
		// Position cursor at end
		a.paramInput.CursorEnd()
		return a, nil
	}

	a.pendingCmd = &cmd
	return a.executeCommand()
}

func (a *App) executeCommand() (tea.Model, tea.Cmd) {
	cmd := a.pendingCmd
	finalCmd := runner.SubstituteParams(cmd.Cmd, a.paramValues)

	a.db.UpdateLastUsed(cmd.ID)

	// Save non-sensitive params
	if len(a.paramInfos) > 0 {
		toSave := make(map[string]string)
		for _, p := range a.paramInfos {
			if !p.Sensitive {
				if v, ok := a.paramValues[p.Name]; ok {
					toSave[p.Name] = v
				}
			}
		}
		a.db.SaveLastParams(cmd.ID, toSave)
	}

	a.running = true
	a.outputLines = []string{cmdPreviewStyle.Render("$ " + finalCmd), ""}
	a.output.SetContent(strings.Join(a.outputLines, "\n"))

	a.mode = modeNormal
	a.searchInput.Focus()
	a.refreshCommands() // reload to get updated last_params

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
	a.editingQuery = nil
}

func (a *App) initQueryForm(q *model.Query) {
	a.formInputs = make([]textinput.Model, 2)

	nameInput := textinput.New()
	nameInput.Placeholder = "Name (e.g., users by date)"
	nameInput.Focus()

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)"

	// SQL textarea
	sqlArea := textarea.New()
	sqlArea.Placeholder = "SELECT * FROM ..."
	sqlArea.ShowLineNumbers = false
	sqlArea.SetHeight(8)

	if q != nil {
		nameInput.SetValue(q.Name)
		sqlArea.SetValue(q.SQL)
		descInput.SetValue(q.Description)
	}

	a.formInputs[0] = nameInput
	a.formInputs[1] = descInput
	a.sqlTextarea = sqlArea
	a.formFocus = 0
	a.editingCmd = nil
}

func (a *App) focusFormInput() tea.Cmd {
	for i := range a.formInputs {
		a.formInputs[i].Blur()
	}
	a.sqlTextarea.Blur()

	if a.tab == tabSQL && a.formFocus == 1 {
		// Focus SQL textarea (index 1 in SQL form is textarea)
		return a.sqlTextarea.Focus()
	}
	return a.formInputs[a.sqlFormInputIndex()].Focus()
}

// sqlFormInputIndex maps formFocus to formInputs index for SQL form
// SQL form: 0=name, 1=textarea, 2=description
// formInputs only has [name, description] for SQL
func (a *App) sqlFormInputIndex() int {
	if a.tab != tabSQL {
		return a.formFocus
	}
	if a.formFocus == 0 {
		return 0 // name
	}
	return 1 // description (formFocus 2 -> index 1)
}

func (a *App) submitForm() (tea.Model, tea.Cmd) {
	if a.tab == tabSQL {
		return a.submitQueryForm()
	}
	return a.submitCommandForm()
}

func (a *App) submitCommandForm() (tea.Model, tea.Cmd) {
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

	dupName, err := a.db.IsDuplicateName(name, excludeID)
	if err != nil {
		a.err = err.Error()
		return a, nil
	}
	if dupName {
		a.err = "A command with this name already exists"
		return a, nil
	}

	dupCmd, err := a.db.IsDuplicateCmd(cmd, excludeID)
	if err != nil {
		a.err = err.Error()
		return a, nil
	}
	if dupCmd {
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

func (a *App) submitQueryForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(a.formInputs[0].Value())
	sql := a.sqlTextarea.Value() // preserve formatting from textarea
	desc := strings.TrimSpace(a.formInputs[1].Value())

	if name == "" || strings.TrimSpace(sql) == "" {
		a.err = "Name and SQL are required"
		return a, nil
	}

	excludeID := int64(0)
	if a.editingQuery != nil {
		excludeID = a.editingQuery.ID
	}

	dupName, err := a.db.IsDuplicateQueryName(name, excludeID)
	if err != nil {
		a.err = err.Error()
		return a, nil
	}
	if dupName {
		a.err = "A query with this name already exists"
		return a, nil
	}

	dupSQL, err := a.db.IsDuplicateQuerySQL(sql, excludeID)
	if err != nil {
		a.err = err.Error()
		return a, nil
	}
	if dupSQL {
		a.err = "A query with this exact SQL already exists"
		return a, nil
	}

	if a.mode == modeAdd {
		_, err = a.db.AddQuery(name, sql, desc)
		if err != nil {
			a.err = err.Error()
			return a, nil
		}
		a.status = "Added!"
	} else {
		err = a.db.UpdateQuery(a.editingQuery.ID, name, sql, desc)
		if err != nil {
			a.err = err.Error()
			return a, nil
		}
		a.status = "Updated!"
	}

	a.refreshQueries()
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

func (a *App) refreshQueries() {
	queries, err := a.db.ListQueries()
	if err != nil {
		a.err = err.Error()
		return
	}
	a.queries = queries
	a.filterQueries()
}

func (a *App) filterItems() {
	if a.tab == tabBash {
		a.filterCommands()
	} else {
		a.filterQueries()
	}
}

func (a *App) filterCommands() {
	query := a.searchInput.Value()
	if query == "" {
		a.filtered = a.commands
		return
	}

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

func (a *App) filterQueries() {
	query := a.searchInput.Value()
	if query == "" {
		a.filteredQueries = a.queries
		return
	}

	var targets []string
	for _, q := range a.queries {
		targets = append(targets, q.Name+" "+q.SQL)
	}

	matches := fuzzy.Find(query, targets)
	a.filteredQueries = make([]model.Query, len(matches))
	for i, m := range matches {
		a.filteredQueries[i] = a.queries[m.Index]
	}

	if a.cursor >= len(a.filteredQueries) {
		a.cursor = max(0, len(a.filteredQueries)-1)
	}
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Title with tabs
	title := titleStyle.Render("cmdbox")
	b.WriteString(title)
	b.WriteString("  ")
	b.WriteString(a.renderTabs())
	b.WriteString("\n\n")

	// Search bar
	searchLabel := helpKeyStyle.Render("S") + helpStyle.Render("earch") + " "
	b.WriteString(searchLabel + a.searchInput.View())
	b.WriteString("\n\n")

	// List
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
	if a.mode == modeDelete && a.listLen() > 0 {
		var name string
		if a.tab == tabBash {
			name = a.filtered[a.cursor].Name
		} else {
			name = a.filteredQueries[a.cursor].Name
		}
		b.WriteString("\n")
		b.WriteString(warningStyle.Render(fmt.Sprintf("Delete '%s'? (y/n)", name)))
		b.WriteString("\n")
	}

	// Param input (inline)
	if a.mode == modeParam {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("Params: "))
		b.WriteString(a.paramInput.View())
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  (edit values inline, enter to run, esc to cancel)"))
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
	if a.tab == tabBash {
		return a.renderCommandList(height)
	}
	return a.renderQueryList(height)
}

func (a *App) renderCommandList(height int) string {
	if len(a.filtered) == 0 {
		return mutedStyle.Render("No commands found. Press 'A' to add one.\n")
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

func (a *App) renderQueryList(height int) string {
	if len(a.filteredQueries) == 0 {
		return mutedStyle.Render("No queries found. Press 'A' to add one.\n")
	}

	var lines []string
	start := 0
	if a.cursor >= height {
		start = a.cursor - height + 1
	}

	end := start + height
	if end > len(a.filteredQueries) {
		end = len(a.filteredQueries)
	}

	for i := start; i < end; i++ {
		q := a.filteredQueries[i]
		prefix := "  "
		style := normalStyle
		if i == a.cursor {
			prefix = "▸ "
			style = selectedStyle
		}

		name := style.Render(prefix + q.Name)
		// Show first line of SQL as preview
		firstLine := strings.Split(q.SQL, "\n")[0]
		preview := cmdPreviewStyle.Render("  " + truncate(firstLine, a.width-10))
		lines = append(lines, name, preview)
	}

	return strings.Join(lines, "\n") + "\n"
}

func (a *App) renderForm() string {
	if a.tab == tabBash {
		return a.renderBashForm()
	}
	return a.renderSQLForm()
}

func (a *App) renderBashForm() string {
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

	b.WriteString(helpStyle.Render("down: next field • enter: save • esc: cancel"))
	b.WriteString("\n")

	return b.String()
}

func (a *App) renderSQLForm() string {
	var b strings.Builder

	title := "Add Query"
	if a.mode == modeEdit {
		title = "Edit Query"
	}
	b.WriteString(labelStyle.Render(title))
	b.WriteString("\n\n")

	// Name field (formFocus 0)
	b.WriteString(labelStyle.Render("Name: "))
	style := inputStyle
	if a.formFocus == 0 {
		style = focusedInputStyle
	}
	b.WriteString(style.Width(a.width - 20).Render(a.formInputs[0].View()))
	b.WriteString("\n\n")

	// SQL textarea (formFocus 1)
	b.WriteString(labelStyle.Render("SQL: "))
	b.WriteString("\n")
	sqlStyle := inputStyle
	if a.formFocus == 1 {
		sqlStyle = focusedInputStyle
	}
	b.WriteString(sqlStyle.Width(a.width - 10).Render(a.sqlTextarea.View()))
	b.WriteString("\n\n")

	// Description field (formFocus 2)
	b.WriteString(labelStyle.Render("Description: "))
	style = inputStyle
	if a.formFocus == 2 {
		style = focusedInputStyle
	}
	b.WriteString(style.Width(a.width - 20).Render(a.formInputs[1].View()))
	b.WriteString("\n\n")

	b.WriteString(helpStyle.Render("down: next field • S: save • esc: cancel"))
	b.WriteString("\n")

	return b.String()
}

func (a *App) renderTabs() string {
	bashTab := "Bash"
	sqlTab := "SQL"

	if a.tab == tabBash {
		bashTab = selectedStyle.Render("[Bash]")
		sqlTab = mutedStyle.Render(" SQL ")
	} else {
		bashTab = mutedStyle.Render(" Bash ")
		sqlTab = selectedStyle.Render("[SQL]")
	}

	return bashTab + " " + sqlTab + "  " + helpStyle.Render("(tab to switch)")
}

func (a *App) renderHelp() string {
	if a.mode != modeNormal {
		return ""
	}

	var parts []string
	if a.tab == tabBash {
		parts = []string{
			helpKeyStyle.Render("enter") + " " + helpStyle.Render("run"),
			helpKeyStyle.Render("A") + helpStyle.Render("dd"),
			helpKeyStyle.Render("E") + helpStyle.Render("dit"),
			helpKeyStyle.Render("D") + helpStyle.Render("elete"),
			helpKeyStyle.Render("Y") + helpStyle.Render("ank"),
			helpKeyStyle.Render("C") + helpStyle.Render("lear"),
			helpKeyStyle.Render("Q") + helpStyle.Render("uit"),
		}
	} else {
		parts = []string{
			helpKeyStyle.Render("enter") + " " + helpStyle.Render("view"),
			helpKeyStyle.Render("A") + helpStyle.Render("dd"),
			helpKeyStyle.Render("E") + helpStyle.Render("dit"),
			helpKeyStyle.Render("D") + helpStyle.Render("elete"),
			helpKeyStyle.Render("Y") + helpStyle.Render("ank"),
			helpKeyStyle.Render("C") + helpStyle.Render("lear"),
			helpKeyStyle.Render("Q") + helpStyle.Render("uit"),
		}
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
