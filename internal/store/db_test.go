package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// The pragmas are set in the DSN rather than run once after opening, because
// they are per-connection and a connection opened later would quietly not have
// them. journal_mode is the exception — it lives in the file — and is checked
// here too, since the weekly backup copies the file while the app is running.
func TestOpenAppliesThePragmas(t *testing.T) {
	db := newTestDB(t)

	var journal string
	if err := db.sql.QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("reading journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}

	var foreignKeys int
	if err := db.sql.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("reading foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := db.sql.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("reading busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

// SQLite ignores the REFERENCES clauses in the schema unless it is asked not
// to, and the schema is Rails' schema precisely so that both images enforce
// the same shape.
func TestForeignKeysAreEnforced(t *testing.T) {
	db := newTestDB(t)

	_, err := db.sql.ExecContext(t.Context(),
		`INSERT INTO start_page_groups (user_id, name, "column", position, created_at, updated_at)
		 VALUES (404, 'Orphan', 1, 0, ?, ?)`,
		railsTime(time.Now().UTC()), railsTime(time.Now().UTC()))
	if err == nil {
		t.Fatalf("a group for a user who does not exist was accepted")
	}
}

// The deferred Rollback is what every move relies on: half a renumbering is
// worse than none of it.
func TestTransactionsRollBackOnError(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	boom := errors.New("boom")

	err := db.tx(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(),
			`INSERT INTO start_page_groups (user_id, name, "column", position, created_at, updated_at)
			 VALUES (?, 'Half Written', 1, 0, ?, ?)`,
			user.ID, railsTime(time.Now().UTC()), railsTime(time.Now().UTC())); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("tx returned %v, want boom", err)
	}

	if names := groupNames(t, db, user.ID, 1); len(names) != 0 {
		t.Errorf("column 1 = %q, want nothing", names)
	}
}

// Every uniqueness rule is checked before the write, so the index only catches
// what the check could not: two requests at the same instant. There is no
// field to blame then, which is what the sentinel is for.
func TestUniqueIndexViolationsBecomeErrConflict(t *testing.T) {
	db := newTestDB(t)
	newUser(t, db, "first@example.com")
	second := newUser(t, db, "second@example.com")

	err := db.update(t.Context(),
		`UPDATE users SET email = ? WHERE id = ?`, "first@example.com", second.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
}

func TestOpenReportsAPathItCannotUse(t *testing.T) {
	_, err := Open(t.Context(), filepath.Join(t.TempDir(), "no", "such", "directory", "db.sqlite3"))
	if err == nil {
		t.Fatalf("opening a database in a directory that does not exist succeeded")
	}
}
