package main

import (
	"fmt"
	"os"

	"cmdbox/db"
	"cmdbox/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	database, err := db.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	app, err := ui.NewApp(database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating app: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		os.Exit(1)
	}
}
