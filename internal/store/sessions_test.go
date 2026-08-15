package store

import (
	"testing"
	"time"
)

func TestCreateSessionDefaultsToTheLifetime(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	session, err := db.CreateSession(t.Context(), user.ID, "Test Agent", "127.0.0.1", time.Time{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	want := time.Now().UTC().Add(SessionLifetime)
	if diff := session.ExpiresAt.Sub(want); diff > time.Second || diff < -time.Second {
		t.Errorf("expires_at = %v, want about %v", session.ExpiresAt, want)
	}
	if session.UserAgent != "Test Agent" || session.IPAddress != "127.0.0.1" {
		t.Errorf("stored %q / %q", session.UserAgent, session.IPAddress)
	}
	if SessionLifetime != 30*24*time.Hour {
		t.Errorf("SessionLifetime = %v, want 30 days", SessionLifetime)
	}
}

func TestActiveSession(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		wantFound bool
	}{
		{"a session with weeks left", time.Now().UTC().Add(20 * 24 * time.Hour), true},
		{"a session with a second left", time.Now().UTC().Add(time.Second), true},
		{"one that ran out a second ago", time.Now().UTC().Add(-time.Second), false},
		{"one that ran out last month", time.Now().UTC().Add(-30 * 24 * time.Hour), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			session, err := db.CreateSession(t.Context(), user.ID, "Test Agent", "127.0.0.1", test.expiresAt)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}

			found, err := db.ActiveSession(t.Context(), session.ID)
			if !test.wantFound {
				assertNotFound(t, err)
				return
			}
			if err != nil {
				t.Fatalf("ActiveSession: %v", err)
			}
			if found.UserID != user.ID {
				t.Errorf("user_id = %d, want %d", found.UserID, user.ID)
			}
		})
	}
}

func TestExtendSession(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	session, err := db.CreateSession(t.Context(), user.ID, "Test Agent", "127.0.0.1",
		time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := db.ExtendSession(t.Context(), session.ID, time.Time{}); err != nil {
		t.Fatalf("ExtendSession: %v", err)
	}

	extended, err := db.ActiveSession(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("ActiveSession: %v", err)
	}
	if !extended.ExpiresAt.After(session.ExpiresAt) {
		t.Errorf("expires_at = %v, want later than %v", extended.ExpiresAt, session.ExpiresAt)
	}
	// created_at is when this browser signed in, and Settings shows it.
	if !extended.CreatedAt.Equal(session.CreatedAt) {
		t.Errorf("created_at moved: %v, was %v", extended.CreatedAt, session.CreatedAt)
	}
}

func TestDeleteSession(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	session, err := db.CreateSession(t.Context(), user.ID, "Test Agent", "127.0.0.1", time.Time{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := db.DeleteSession(t.Context(), session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	_, err = db.ActiveSession(t.Context(), session.ID)
	assertNotFound(t, err)

	// Signing out of a session that is already gone is not an error the page
	// could do anything with, but it is not a silent success either.
	assertNotFound(t, db.DeleteSession(t.Context(), session.ID))
}

// Sessions are tidied when someone signs in, so the table cannot grow without
// bound and there is no cron job to forget about.
func TestDeleteExpiredSessions(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")

	if _, err := db.CreateSession(t.Context(), user.ID, "Old", "127.0.0.1",
		time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	live, err := db.CreateSession(t.Context(), user.ID, "New", "127.0.0.1", time.Time{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	theirs, err := db.CreateSession(t.Context(), other.ID, "Theirs", "127.0.0.1",
		time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := db.DeleteExpiredSessions(t.Context(), user.ID); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}

	sessions := sessionsForUser(t, db, user.ID)
	if len(sessions) != 1 || sessions[0].ID != live.ID {
		t.Errorf("kept %d sessions, want only %d", len(sessions), live.ID)
	}

	// Someone else's expired session is not this user's to clear up.
	sessions = sessionsForUser(t, db, other.ID)
	if len(sessions) != 1 || sessions[0].ID != theirs.ID {
		t.Errorf("another user's session was deleted")
	}
}

// user_agent and ip_address are nullable, and Rails wrote NULL when the
// request carried neither. Both come back as the empty string.
func TestSessionWithoutAUserAgent(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	if _, err := db.sql.ExecContext(t.Context(),
		`INSERT INTO sessions (user_id, user_agent, ip_address, expires_at, created_at, updated_at)
		 VALUES (?, NULL, NULL, ?, ?, ?)`,
		user.ID, railsTime(time.Now().UTC().Add(time.Hour)),
		railsTime(time.Now().UTC()), railsTime(time.Now().UTC())); err != nil {
		t.Fatalf("inserting: %v", err)
	}

	sessions := sessionsForUser(t, db, user.ID)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].UserAgent != "" || sessions[0].IPAddress != "" {
		t.Errorf("got %q / %q, want empty strings", sessions[0].UserAgent, sessions[0].IPAddress)
	}
}
