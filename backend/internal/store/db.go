package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DB wraps *sql.DB to transparently convert ? placeholders to $N (PostgreSQL style).
// This lets all handler code keep using ? without changes.
type DB struct {
	*sql.DB
}

// convertPlaceholders replaces ? with $1, $2, ... for PostgreSQL.
// It skips ? inside single-quoted strings.
func convertPlaceholders(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	inString := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			inString = !inString
			b.WriteByte(ch)
		} else if ch == '?' && !inString {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// Tx wraps *sql.Tx to transparently convert ? placeholders to $N (PostgreSQL style).
type Tx struct {
	*sql.Tx
}

func (d *DB) BeginTx(ctx context.Context) (*Tx, error) {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx}, nil
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(convertPlaceholders(query), args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(convertPlaceholders(query), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(convertPlaceholders(query), args...)
}

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(convertPlaceholders(query), args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(convertPlaceholders(query), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(convertPlaceholders(query), args...)
}
