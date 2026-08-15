package store

import (
	"testing"
	"time"
)

func TestConnectionForUser(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	// The ordinary state of an account that has not connected anything.
	_, err := db.ConnectionForUser(t.Context(), user.ID)
	assertNotFound(t, err)

	expires := time.Now().UTC().Add(90 * 24 * time.Hour)
	created, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://links.example.com", "a-token", "search,visit", expires)
	if err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}

	stored, err := db.ConnectionForUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ConnectionForUser: %v", err)
	}
	if stored.ID != created.ID {
		t.Errorf("id = %d, want %d", stored.ID, created.ID)
	}
	if stored.BaseURL != "https://links.example.com" || stored.Token != "a-token" {
		t.Errorf("stored %q / %q", stored.BaseURL, stored.Token)
	}
	if stored.Scopes != "search,visit" {
		t.Errorf("scopes = %q", stored.Scopes)
	}
	if !stored.TokenExpiresAt.Equal(expires.Truncate(time.Microsecond)) {
		t.Errorf("token_expires_at = %v, want %v", stored.TokenExpiresAt, expires)
	}
	if stored.NeedsReconnect() {
		t.Errorf("a fresh connection should not need reconnecting")
	}
}

func TestReplaceConnectionValidations(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		token   string
		want    []string
	}{
		{"no base url", "", "a-token", []string{"Base url can't be blank"}},
		{"no token", "https://links.example.com", "", []string{"Token can't be blank"}},
		{"neither", "", "", []string{"Base url can't be blank", "Token can't be blank"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			_, err := db.ReplaceConnection(t.Context(), user.ID, test.baseURL, test.token, "", time.Time{})
			assertInvalid(t, err, test.want...)
		})
	}
}

// A new grant is a different token with different scopes and no history worth
// keeping — and the unique index means there is only ever one to replace.
func TestReplaceConnectionKeepsOnlyTheLatest(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	if _, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://old.example.com", "old-token", "search", time.Time{}); err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}
	if _, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://new.example.com", "new-token", "search,visit", time.Time{}); err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}

	var count int
	if err := db.sql.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM connections WHERE user_id = ?`, user.ID).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Errorf("%d connections, want 1", count)
	}

	stored, err := db.ConnectionForUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ConnectionForUser: %v", err)
	}
	if stored.Token != "new-token" {
		t.Errorf("token = %q, want new-token", stored.Token)
	}
	// No expiry given means NULL, not the year 1.
	if !stored.TokenExpiresAt.IsZero() {
		t.Errorf("token_expires_at = %v, want the zero time", stored.TokenExpiresAt)
	}
}

// Scoped to a user on purpose: a token grants access to exactly one account on
// the other app.
func TestConnectionsAreOnePerUser(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")

	if _, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://links.example.com", "mine", "", time.Time{}); err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}
	if _, err := db.ReplaceConnection(t.Context(), other.ID,
		"https://other.example.com", "theirs", "", time.Time{}); err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}

	mine, err := db.ConnectionForUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ConnectionForUser: %v", err)
	}
	if mine.Token != "mine" {
		t.Errorf("token = %q, want mine", mine.Token)
	}
}

// A lapsed token and an empty archive look identical otherwise, so the start
// page has to be able to say "reconnect".
func TestRecordAndClearConnectionFailure(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	connection, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://links.example.com", "a-token", "", time.Time{})
	if err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}

	if err := db.RecordConnectionFailure(t.Context(), connection.ID, "token rejected"); err != nil {
		t.Fatalf("RecordConnectionFailure: %v", err)
	}

	failed, err := db.ConnectionForUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ConnectionForUser: %v", err)
	}
	if !failed.NeedsReconnect() || failed.LastError != "token rejected" {
		t.Errorf("last_error = %q, needs reconnect = %v", failed.LastError, failed.NeedsReconnect())
	}
	if failed.LastFailedAt.IsZero() {
		t.Errorf("last_failed_at was not set")
	}

	if err := db.ClearConnectionFailure(t.Context(), connection.ID); err != nil {
		t.Fatalf("ClearConnectionFailure: %v", err)
	}

	cleared, err := db.ConnectionForUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ConnectionForUser: %v", err)
	}
	if cleared.NeedsReconnect() || cleared.LastError != "" || !cleared.LastFailedAt.IsZero() {
		t.Errorf("the failure survived: %+v", cleared)
	}
}

// A search runs on every keystroke in the command bar, and each one clears the
// failure. Without the guard every keystroke would be a write.
func TestClearConnectionFailureWritesNothingWhenThereIsNothingToClear(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	connection, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://links.example.com", "a-token", "", time.Time{})
	if err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}

	before := totalChanges(t, db)
	if err := db.ClearConnectionFailure(t.Context(), connection.ID); err != nil {
		t.Fatalf("ClearConnectionFailure: %v", err)
	}
	if after := totalChanges(t, db); after != before {
		t.Errorf("%d rows were written, want none", after-before)
	}
}

func TestDeleteConnection(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	if _, err := db.ReplaceConnection(t.Context(), user.ID,
		"https://links.example.com", "a-token", "", time.Time{}); err != nil {
		t.Fatalf("ReplaceConnection: %v", err)
	}

	if err := db.DeleteConnection(t.Context(), user.ID); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}
	_, err := db.ConnectionForUser(t.Context(), user.ID)
	assertNotFound(t, err)

	// The button is there whether or not there is anything behind it.
	if err := db.DeleteConnection(t.Context(), user.ID); err != nil {
		t.Errorf("disconnecting twice: %v", err)
	}
}

func TestConnectionFailureOnAConnectionThatIsGone(t *testing.T) {
	db := newTestDB(t)

	assertNotFound(t, db.RecordConnectionFailure(t.Context(), 404, "token rejected"))
	assertNotFound(t, db.ClearConnectionFailure(t.Context(), 404))
}
