package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain turns the hashing cost down for the whole package. Rails does the
// same thing in its test environment, for the same reason. At the real cost a
// single sign-up takes a quarter of a second, and these tests create dozens of
// accounts. TestCreateUserHashesAtRailsCost puts it back for the one test that
// cares what the cost actually is.
func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// newTestDB is a migrated, empty database in a temporary directory. It uses a
// file rather than :memory:, because the file is what production has. WAL,
// busy_timeout and the rest only mean anything on one.
func newTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrating the test database: %v", err)
	}
	return db
}

// newUser signs someone up with a three-column grid, which is what the Rails
// fixtures gave users(:one) and users(:two). The group and tile tests need
// somewhere to move things to.
func newUser(t *testing.T, db *DB, email string) *User {
	t.Helper()

	user, err := db.CreateUser(t.Context(), email, "password123")
	if err != nil {
		t.Fatalf("creating %s: %v", email, err)
	}
	if err := db.UpdateColumns(t.Context(), user.ID, 3); err != nil {
		t.Fatalf("widening %s to three columns: %v", email, err)
	}
	user.Columns = 3
	return user
}

func newGroup(t *testing.T, db *DB, userID int64, name string, column int) *Group {
	t.Helper()

	group, err := db.CreateGroup(t.Context(), userID, name, column)
	if err != nil {
		t.Fatalf("creating group %q: %v", name, err)
	}
	return group
}

func newItem(t *testing.T, db *DB, userID, groupID int64, title, url string) *Item {
	t.Helper()

	item, err := db.CreateItem(t.Context(), userID, groupID, title, url)
	if err != nil {
		t.Fatalf("creating item %q: %v", title, err)
	}
	return item
}

// groupNames is the column in the order the page draws it.
func groupNames(t *testing.T, db *DB, userID int64, column int) []string {
	t.Helper()

	groups, err := db.GroupsInColumn(t.Context(), userID, column)
	if err != nil {
		t.Fatalf("reading column %d: %v", column, err)
	}
	names := make([]string, len(groups))
	for i, group := range groups {
		names[i] = group.Name
	}
	return names
}

// groupPositions is the same column as position numbers, for the assertions
// about gaps being closed.
func groupPositions(t *testing.T, db *DB, userID int64, column int) []int {
	t.Helper()

	groups, err := db.GroupsInColumn(t.Context(), userID, column)
	if err != nil {
		t.Fatalf("reading column %d: %v", column, err)
	}
	positions := make([]int, len(groups))
	for i, group := range groups {
		positions[i] = group.Position
	}
	return positions
}

func itemTitles(t *testing.T, db *DB, groupID int64) []string {
	t.Helper()

	items, err := db.ItemsInGroup(t.Context(), groupID)
	if err != nil {
		t.Fatalf("reading group %d: %v", groupID, err)
	}
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = item.Title
	}
	return titles
}

func itemPositions(t *testing.T, db *DB, groupID int64) []int {
	t.Helper()

	items, err := db.ItemsInGroup(t.Context(), groupID)
	if err != nil {
		t.Fatalf("reading group %d: %v", groupID, err)
	}
	positions := make([]int, len(items))
	for i, item := range items {
		positions[i] = item.Position
	}
	return positions
}

// assertInvalid is the shape almost every refusal test uses. The error has to
// be a ValidationError, and its full messages have to be exactly the ones the
// page used to print. Comparing the whole list rather than "contains" is
// deliberate — the editor joins them with ", ", so an extra message is a
// changed page.
func assertInvalid(t *testing.T, err error, want ...string) {
	t.Helper()

	var invalid ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a ValidationError, got %v", err)
	}
	got := invalid.FullMessages()
	if len(got) != len(want) {
		t.Fatalf("messages = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("messages = %q, want %q", got, want)
		}
	}
}

func assertNotFound(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func assertEqualStrings(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func assertEqualInts(t *testing.T, got, want []int) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// totalChanges is SQLite's count of rows inserted, updated or deleted on this
// connection. The store keeps one connection, so the number is stable. That is
// what makes "this operation writes nothing" a testable claim rather than an
// assertion about the implementation.
func totalChanges(t *testing.T, db *DB) int {
	t.Helper()

	var changes int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT total_changes()`).Scan(&changes); err != nil {
		t.Fatalf("reading total_changes(): %v", err)
	}
	return changes
}

// sessionsForUser reads a user's sessions straight out of the table, newest
// first. It is a test helper rather than a method on DB because nothing in the
// app lists sessions — the tests are the only reader there is.
func sessionsForUser(t *testing.T, db *DB, userID int64) []Session {
	t.Helper()

	rows, err := db.sql.QueryContext(t.Context(),
		`SELECT `+sessionColumns+` FROM sessions WHERE user_id = ? ORDER BY created_at DESC, id DESC`,
		userID)
	if err != nil {
		t.Fatalf("reading sessions: %v", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			t.Fatalf("scanning a session: %v", err)
		}
		sessions = append(sessions, *session)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading sessions: %v", err)
	}
	return sessions
}

// loadSQL runs a file of statements against the database, which is how the
// captured Rails rows get in.
func loadSQL(t *testing.T, db *DB, path string) {
	t.Helper()

	statements, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), string(statements)); err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
}
