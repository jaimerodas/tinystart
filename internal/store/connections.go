package store

import (
	"context"
	"database/sql"
	"net/url"
	"time"
)

// Connection is one user's credential for another app, obtained through that
// app's device flow. It is scoped to a user on purpose: the token grants
// access to exactly one account over there, so it must only ever serve the
// person who approved it.
//
// LastFailedAt and LastError are here so the start page can say "reconnect"
// instead of quietly showing no federated results — a lapsed token and an
// empty archive look identical otherwise.
type Connection struct {
	ID             int64
	UserID         int64
	BaseURL        string
	Token          string
	Scopes         string
	TokenExpiresAt time.Time
	LastFailedAt   time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NeedsReconnect is Connection#needs_reconnect?: a recorded error is what the
// page reads, not the timestamp beside it.
func (c *Connection) NeedsReconnect() bool { return c.LastError != "" }

// Hostname is the host out of BaseURL, which is what the pages call the other
// app: "links.pati.to", not "https://links.pati.to". A URL that will not parse
// has no host to show, and the pages that use this simply say nothing — the
// same empty string Connection#hostname's rescue produced.
func (c *Connection) Hostname() string {
	parsed, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

const connectionColumns = `id, user_id, base_url, token, scopes, token_expires_at,
	last_failed_at, last_error, created_at, updated_at`

// ConnectionForUser returns the connection, or ErrNotFound when there is none
// — which is the ordinary state of an account that has not connected anything.
func (db *DB) ConnectionForUser(ctx context.Context, userID int64) (*Connection, error) {
	return scanConnection(db.sql.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM connections WHERE user_id = ?`, userID))
}

// ReplaceConnection stores a freshly approved grant, throwing away whatever was
// there. Replace rather than update because a new grant is a different token
// with different scopes and no history worth keeping — and because the unique
// index on user_id means there is only ever one to replace.
func (db *DB) ReplaceConnection(ctx context.Context, userID int64, baseURL, token, scopes string, tokenExpiresAt time.Time) (*Connection, error) {
	var fields []FieldError
	if baseURL == "" {
		fields = append(fields, FieldError{"base_url", msgBlank})
	}
	if token == "" {
		fields = append(fields, FieldError{"token", msgBlank})
	}
	if err := invalid(fields...); err != nil {
		return nil, err
	}

	var connection *Connection
	err := db.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM connections WHERE user_id = ?`, userID); err != nil {
			return err
		}

		now := time.Now().UTC()
		result, err := tx.ExecContext(ctx,
			`INSERT INTO connections (user_id, base_url, token, scopes, token_expires_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			userID, baseURL, token, nullString(scopes), railsTime(tokenExpiresAt),
			railsTime(now), railsTime(now))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}

		connection = &Connection{ID: id, UserID: userID, BaseURL: baseURL, Token: token,
			Scopes: scopes, TokenExpiresAt: tokenExpiresAt.UTC().Truncate(time.Microsecond),
			CreatedAt: now, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connection, nil
}

// DeleteConnection is "Disconnect". Deleting nothing is not an error: the
// button is there whether or not the grant survived on the other side.
func (db *DB) DeleteConnection(ctx context.Context, userID int64) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM connections WHERE user_id = ?`, userID)
	return err
}

// RecordConnectionFailure notes that the other app turned the token down, so
// the start page can offer a reconnect. updated_at is deliberately untouched —
// Rails used update_columns here, and this is a note about the token rather
// than a change to it.
func (db *DB) RecordConnectionFailure(ctx context.Context, connectionID int64, message string) error {
	return db.update(ctx,
		`UPDATE connections SET last_failed_at = ?, last_error = ? WHERE id = ?`,
		railsTime(time.Now().UTC()), message, connectionID)
}

// ClearConnectionFailure forgets a recorded failure, and writes nothing when
// there is nothing recorded. That guard is not an optimisation: a search runs
// on every keystroke in the command bar, and without it every one of them
// would be a write to the database.
func (db *DB) ClearConnectionFailure(ctx context.Context, connectionID int64) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		var lastError, lastFailedAt sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT last_error, last_failed_at FROM connections WHERE id = ?`,
			connectionID).Scan(&lastError, &lastFailedAt)
		if err != nil {
			return notFound(err)
		}
		if !lastError.Valid && !lastFailedAt.Valid {
			return nil
		}

		_, err = tx.ExecContext(ctx,
			`UPDATE connections SET last_failed_at = NULL, last_error = NULL WHERE id = ?`,
			connectionID)
		return err
	})
}

// nullString keeps an empty optional column NULL rather than an empty
// string, so that a connection Go wrote and one Rails wrote look the same.
func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func scanConnection(row scanner) (*Connection, error) {
	var (
		connection Connection
		scopes     sql.NullString
		lastError  sql.NullString
	)
	err := row.Scan(&connection.ID, &connection.UserID, &connection.BaseURL, &connection.Token,
		&scopes, (*railsTime)(&connection.TokenExpiresAt), (*railsTime)(&connection.LastFailedAt),
		&lastError, (*railsTime)(&connection.CreatedAt), (*railsTime)(&connection.UpdatedAt))
	if err != nil {
		return nil, notFound(err)
	}
	connection.Scopes = scopes.String
	connection.LastError = lastError.String
	return &connection, nil
}
