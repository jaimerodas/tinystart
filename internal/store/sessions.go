package store

import (
	"context"
	"database/sql"
	"time"
)

// Session is one signed-in browser. The cookie carries the id and a signature
// over it; the row is what makes signing out somewhere else take effect here.
type Session struct {
	ID        int64
	UserID    int64
	UserAgent string
	IPAddress string
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionLifetime is how long a session lasts from the moment it is created or
// extended. It lives here rather than in the web layer because CreateSession
// falls back to it, which is the same thing Session's after_create callback
// did.
const SessionLifetime = 30 * 24 * time.Hour

const sessionColumns = `id, user_id, user_agent, ip_address, expires_at, created_at, updated_at`

// CreateSession records a sign-in. A zero expiresAt means "the usual", which
// is what every caller wants and what Rails' set_initial_expiration did.
func (db *DB) CreateSession(ctx context.Context, userID int64, userAgent, ip string, expiresAt time.Time) (*Session, error) {
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(SessionLifetime)
	}

	now := time.Now().UTC()
	result, err := db.sql.ExecContext(ctx,
		`INSERT INTO sessions (user_id, user_agent, ip_address, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, userAgent, ip, railsTime(expiresAt), railsTime(now), railsTime(now))
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:        id,
		UserID:    userID,
		UserAgent: userAgent,
		IPAddress: ip,
		ExpiresAt: expiresAt.UTC().Truncate(time.Microsecond),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ActiveSession is the one lookup the authentication middleware makes on every
// request: a session that exists and has not expired. An expired one is
// ErrNotFound, not a Session with a flag on it — there is nothing a caller
// could usefully do with it, and returning it invites forgetting to check.
//
// The expiry is compared as text, which is sound because railsTime writes a
// fixed-width, zero-padded, UTC timestamp: sorting those as strings is sorting
// them as instants.
func (db *DB) ActiveSession(ctx context.Context, id int64) (*Session, error) {
	return scanSession(db.sql.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE id = ? AND expires_at >= ?`,
		id, railsTime(time.Now().UTC())))
}

// ExtendSession pushes the expiry out. The web layer calls it when a session
// is within a week of running out, so that someone who visits every day is
// never signed out and someone who disappears for a month is.
func (db *DB) ExtendSession(ctx context.Context, id int64, expiresAt time.Time) error {
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(SessionLifetime)
	}
	// created_at is deliberately untouched: it is when this browser signed in,
	// and the Settings page shows it.
	return db.update(ctx, `UPDATE sessions SET expires_at = ?, updated_at = ? WHERE id = ?`,
		railsTime(expiresAt), railsTime(time.Now().UTC()), id)
}

// DeleteSession is signing out.
func (db *DB) DeleteSession(ctx context.Context, id int64) error {
	return db.update(ctx, `DELETE FROM sessions WHERE id = ?`, id)
}

// DeleteExpiredSessions clears out one user's dead sessions. Called when they
// sign in, so the table cannot grow without bound and there is no cron job to
// forget about.
func (db *DB) DeleteExpiredSessions(ctx context.Context, userID int64) error {
	_, err := db.sql.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND expires_at < ?`,
		userID, railsTime(time.Now().UTC()))
	return err
}

// scanSession reads one row. user_agent and ip_address are nullable — Rails
// stores NULL when the request carried neither — and both come back as the
// empty string, because every caller treats "missing" and "empty" the same
// way and a *string would make them all say so.
func scanSession(row scanner) (*Session, error) {
	var (
		session   Session
		userAgent sql.NullString
		ip        sql.NullString
	)
	err := row.Scan(&session.ID, &session.UserID, &userAgent, &ip,
		(*railsTime)(&session.ExpiresAt), (*railsTime)(&session.CreatedAt),
		(*railsTime)(&session.UpdatedAt))
	if err != nil {
		return nil, notFound(err)
	}
	session.UserAgent = userAgent.String
	session.IPAddress = ip.String
	return &session, nil
}
