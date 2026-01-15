package db

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cmdbox/model"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func New() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".cmdbox")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dir, "commands.db")
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, err
	}

	return db, nil
}

func (d *DB) migrate() error {
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS commands (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cmd TEXT NOT NULL,
			description TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_commands_name ON commands(name);
		CREATE INDEX IF NOT EXISTS idx_commands_cmd ON commands(cmd);
	`)
	if err != nil {
		return err
	}

	// Add last_params column if not exists
	_, err = d.conn.Exec(`ALTER TABLE commands ADD COLUMN last_params TEXT DEFAULT ''`)
	// Ignore error if column already exists
	return nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) List() ([]model.Command, error) {
	rows, err := d.conn.Query(`
		SELECT id, name, cmd, description, created_at, last_used_at, COALESCE(last_params, '')
		FROM commands
		ORDER BY last_used_at DESC NULLS LAST, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commands []model.Command
	for rows.Next() {
		var c model.Command
		var lastUsed sql.NullTime
		if err := rows.Scan(&c.ID, &c.Name, &c.Cmd, &c.Description, &c.CreatedAt, &lastUsed, &c.LastParams); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			c.LastUsedAt = &lastUsed.Time
		}
		commands = append(commands, c)
	}
	return commands, rows.Err()
}

func (d *DB) Add(name, cmd, description string) (int64, error) {
	result, err := d.conn.Exec(
		`INSERT INTO commands (name, cmd, description) VALUES (?, ?, ?)`,
		name, cmd, description,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (d *DB) Update(id int64, name, cmd, description string) error {
	_, err := d.conn.Exec(
		`UPDATE commands SET name = ?, cmd = ?, description = ? WHERE id = ?`,
		name, cmd, description, id,
	)
	return err
}

func (d *DB) Delete(id int64) error {
	_, err := d.conn.Exec(`DELETE FROM commands WHERE id = ?`, id)
	return err
}

func (d *DB) UpdateLastUsed(id int64) error {
	_, err := d.conn.Exec(
		`UPDATE commands SET last_used_at = ? WHERE id = ?`,
		time.Now(), id,
	)
	return err
}

// IsDuplicateCmd checks if a command with the same cmd string exists
func (d *DB) IsDuplicateCmd(cmd string, excludeID int64) (bool, error) {
	normalized := strings.TrimSpace(cmd)
	var count int
	err := d.conn.QueryRow(
		`SELECT COUNT(*) FROM commands WHERE TRIM(cmd) = ? AND id != ?`,
		normalized, excludeID,
	).Scan(&count)
	return count > 0, err
}

// IsDuplicateName checks if a command with the same name exists
func (d *DB) IsDuplicateName(name string, excludeID int64) (bool, error) {
	normalized := strings.TrimSpace(name)
	var count int
	err := d.conn.QueryRow(
		`SELECT COUNT(*) FROM commands WHERE TRIM(name) = ? AND id != ?`,
		normalized, excludeID,
	).Scan(&count)
	return count > 0, err
}

// SaveLastParams saves param values as JSON (caller should filter sensitive params)
func (d *DB) SaveLastParams(id int64, params map[string]string) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(`UPDATE commands SET last_params = ? WHERE id = ?`, string(data), id)
	return err
}
