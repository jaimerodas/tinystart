package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ActiveRecord wrote the rows in testdata/rails_rows.sql, copied out of the
// development database. Reading them is the check that the store understands
// what is already on disk. That includes the timestamps, the 0/1 booleans,
// and the NULLs, not only what it wrote itself.
func TestReadsRowsRailsWrote(t *testing.T) {
	db := newTestDB(t)
	loadSQL(t, db, filepath.Join("testdata", "rails_rows.sql"))

	user, err := db.UserByID(t.Context(), 1)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.Email != "someone@example.com" {
		t.Errorf("email = %q", user.Email)
	}
	if !user.Admin || !user.Approved {
		t.Errorf("admin = %v, approved = %v, want both true", user.Admin, user.Approved)
	}
	if user.Columns != 3 || user.ColorPreference != "blue" || user.ThemePreference != "system" {
		t.Errorf("preferences: %d columns, %q, %q", user.Columns, user.ColorPreference, user.ThemePreference)
	}
	wantCreated := time.Date(2026, 8, 7, 22, 36, 4, 233339000, time.UTC)
	if !user.CreatedAt.Equal(wantCreated) {
		t.Errorf("created_at = %v, want %v", user.CreatedAt, wantCreated)
	}

	// The password in the fixture is the one every Rails test used. The Ruby
	// bcrypt gem hashed it at cost 12.
	if _, err := db.Authenticate(t.Context(), "someone@example.com", "password123"); err != nil {
		t.Errorf("Authenticate against the captured digest: %v", err)
	}

	byColumn, err := db.GroupsByColumn(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("GroupsByColumn: %v", err)
	}
	if len(byColumn[1]) != 1 || byColumn[1][0].Name != "Lo de siempre" {
		t.Errorf("column 1 = %v", byColumn[1])
	}
	if len(byColumn[2]) != 1 || byColumn[2][0].Name != "Mis proyectitos" {
		t.Errorf("column 2 = %v", byColumn[2])
	}

	assertEqualStrings(t, itemTitles(t, db, 15), []string{"Fastmail", "Feedbin"})
	assertEqualStrings(t, itemTitles(t, db, 17), []string{"Links Patito"})

	// Read the row directly rather than through ActiveSession: the captured
	// row has a real expiry on it, and a test that quietly changes meaning on
	// a particular Tuesday is worse than no test.
	sessions := sessionsForUser(t, db, user.ID)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].IPAddress != "203.0.113.7" {
		t.Errorf("ip_address = %q", sessions[0].IPAddress)
	}
	wantExpiry := time.Date(2026, 9, 6, 22, 36, 13, 267704000, time.UTC)
	if !sessions[0].ExpiresAt.Equal(wantExpiry) {
		t.Errorf("expires_at = %v, want %v", sessions[0].ExpiresAt, wantExpiry)
	}

	connection, err := db.ConnectionForUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ConnectionForUser: %v", err)
	}
	if connection.Scopes != "search,visit" {
		t.Errorf("scopes = %q", connection.Scopes)
	}
	// NULL columns read as the zero value, not as an error.
	if connection.LastError != "" || !connection.LastFailedAt.IsZero() {
		t.Errorf("last_error = %q, last_failed_at = %v", connection.LastError, connection.LastFailedAt)
	}
	// The one timestamp Rails wrote without a fractional part.
	wantTokenExpiry := time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC)
	if !connection.TokenExpiresAt.Equal(wantTokenExpiry) {
		t.Errorf("token_expires_at = %v, want %v", connection.TokenExpiresAt, wantTokenExpiry)
	}
}

// Reading a row Rails wrote and writing it back has to leave the text on disk
// byte for byte as it was. Anything else and the two images write two
// different formats into the same column.
func TestRewritesRailsTimestampsUnchanged(t *testing.T) {
	db := newTestDB(t)
	loadSQL(t, db, filepath.Join("testdata", "rails_rows.sql"))

	for _, test := range []struct {
		column string
		want   string
	}{
		{"created_at", "2026-08-10 17:46:46.161865"},
		{"token_expires_at", "2026-11-08 17:46:42"},
	} {
		var before string
		if err := db.sql.QueryRowContext(t.Context(),
			`SELECT `+test.column+` FROM connections WHERE id = 3`).Scan(&before); err != nil {
			t.Fatalf("reading %s: %v", test.column, err)
		}
		if before != test.want {
			t.Fatalf("%s started as %q, want %q", test.column, before, test.want)
		}

		var when railsTime
		if err := when.Scan(before); err != nil {
			t.Fatalf("scanning %s: %v", test.column, err)
		}
		after, err := when.Value()
		if err != nil {
			t.Fatalf("re-formatting %s: %v", test.column, err)
		}
		if after != before {
			t.Errorf("%s came back as %v, want %q", test.column, after, before)
		}
	}
}

// The real thing, when it is around. storage/development.sqlite3 is not in the
// repository — it is a working database on a laptop. So this test skips
// wherever it is absent, and is a genuine end-to-end read of a Rails-written
// file wherever it is not. It never opens the original: WAL or not, a test
// has no business writing to it.
func TestOpensACopyOfTheDevelopmentDatabase(t *testing.T) {
	source := filepath.Join("..", "..", "storage", "development.sqlite3")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Skipf("no development database to read: %v", err)
	}

	copied := filepath.Join(t.TempDir(), "development.sqlite3")
	if err := os.WriteFile(copied, original, 0o600); err != nil {
		t.Fatalf("copying it: %v", err)
	}

	db, err := Open(t.Context(), copied)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Migrating an existing database changes nothing.
	before := totalChanges(t, db)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if after := totalChanges(t, db); after != before {
		t.Errorf("%d rows were written to an existing database", after-before)
	}

	users, err := db.AllUsers(t.Context())
	if err != nil {
		t.Fatalf("AllUsers: %v", err)
	}
	if len(users) == 0 {
		t.Fatalf("no users in the development database")
	}

	for _, user := range users {
		if user.CreatedAt.IsZero() {
			t.Errorf("%s has no created_at", user.Email)
		}
		byColumn, err := db.GroupsByColumn(t.Context(), user.ID)
		if err != nil {
			t.Fatalf("GroupsByColumn: %v", err)
		}
		for column, groups := range byColumn {
			if column < 1 || column > user.Columns {
				t.Errorf("%s has a group in column %d of %d", user.Email, column, user.Columns)
			}
			for _, group := range groups {
				if _, err := db.ItemsInGroup(t.Context(), group.ID); err != nil {
					t.Fatalf("ItemsInGroup(%q): %v", group.Name, err)
				}
			}
		}
		if _, err := db.LinksForCommandBar(t.Context(), user.ID); err != nil {
			t.Fatalf("LinksForCommandBar: %v", err)
		}
	}
}
