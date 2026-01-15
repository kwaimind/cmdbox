package model

import "time"

type Command struct {
	ID          int64
	Name        string
	Cmd         string
	Description string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	LastParams  string // JSON map of last-used param values
}
