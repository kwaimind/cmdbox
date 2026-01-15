# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and run

```bash
go build -o cmdbox .   # build
./cmdbox               # run
go run .               # build and run
```

No tests currently exist.

## Architecture

cmdbox is a TUI app for saving/running shell commands. Built with Bubble Tea (Elm architecture).

**Packages:**
- `model/` - Data types (`Command` struct)
- `db/` - SQLite persistence (stored at `~/.cmdbox/commands.db`)
- `runner/` - Command execution with `{{param}}` substitution, streams output via channels
- `ui/` - Bubble Tea app (state machine with modes: normal, add, edit, delete, param)

**Data flow:** User input -> Update() -> state changes -> View() renders. Command output streams through `runner.OutputMsg` channel to viewport.

**Key patterns:**
- Commands support `{{paramName}}` placeholders - prompts user for values at runtime
- Fuzzy search filters commands by name+cmd text
- Commands sorted by last_used_at, then created_at
