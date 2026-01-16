package model

import "time"

type Query struct {
	ID          int64
	Name        string
	SQL         string
	Description string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
}
