package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A database Rails set up — testdata/rails_schema.sql, the captured output of
// `sqlite3 storage/development.sqlite3 .schema` against the real thing — and
// a fresh database Go migrates from nothing have to end up with the identical
// schema, statement for statement, once Migrate has run on both. Production
// is the first shape and a new install is the second, and nothing tells them
// apart afterwards.
func TestMigrateProducesTheRailsSchema(t *testing.T) {
	fresh := newTestDB(t)

	rails, err := Open(t.Context(), filepath.Join(t.TempDir(), "rails.sqlite3"))
	if err != nil {
		t.Fatalf("opening the rails-shaped database: %v", err)
	}
	t.Cleanup(func() { rails.Close() })

	captured, err := os.ReadFile(filepath.Join("testdata", "rails_schema.sql"))
	if err != nil {
		t.Fatalf("reading the captured schema: %v", err)
	}
	execCapturedSchema(t, rails, string(captured))

	if err := rails.Migrate(t.Context()); err != nil {
		t.Fatalf("migrating the rails-shaped database: %v", err)
	}

	got := schemaStatements(t, rails)
	want := schemaStatements(t, fresh)
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d\n%s", len(got), len(want), diffStatements(got, want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// Rails recorded every migration it ran, and a database Go creates has to
// carry all eleven, so it is indistinguishable from the database Rails left
// behind. Migrate also reads this table to decide which files in migrations/
// are pending, and by now one of them has run: schema_migrations carries that
// version too.
func TestMigrateRecordsEveryMigrationVersion(t *testing.T) {
	db := newTestDB(t)

	versions, err := db.appliedVersions(t.Context())
	if err != nil {
		t.Fatalf("appliedVersions: %v", err)
	}

	for _, version := range railsMigrations {
		if !slices.Contains(versions, version) {
			t.Errorf("missing rails version %s", version)
		}
	}

	want := append(slices.Clone(railsMigrations), "20260827180000")
	slices.Sort(want)
	assertEqualStrings(t, versions, want)

	if len(want) != 12 {
		t.Errorf("%d versions, want the eleven in db/migrate plus the one migration added since", len(want))
	}
}

func TestMigrateRecordsTheEnvironment(t *testing.T) {
	db := newTestDB(t)

	rows, err := db.sql.QueryContext(t.Context(),
		`SELECT key, value, created_at, updated_at FROM ar_internal_metadata`)
	if err != nil {
		t.Fatalf("querying ar_internal_metadata: %v", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key, value, createdAt, updatedAt string
		if err := rows.Scan(&key, &value, &createdAt, &updatedAt); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		keys = append(keys, key)
		if key == "environment" && value != "production" {
			t.Errorf("environment = %q, want production", value)
		}
		// railsTime wrote this value, so it has to read back as one.
		var when railsTime
		if err := when.Scan(createdAt); err != nil {
			t.Errorf("created_at %q: %v", createdAt, err)
		}
	}
	assertEqualStrings(t, keys, []string{"environment"})
}

// Every real database already has the tables, because Rails already ran in
// production. Migrating one has to be a no-op, and a second boot has to be
// a no-op too.
func TestMigrateIsIdempotent(t *testing.T) {
	db := newTestDB(t)

	before := totalChanges(t, db)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if after := totalChanges(t, db); after != before {
		t.Errorf("%d rows were written on the second migrate, want none", after-before)
	}

	versions, err := db.appliedVersions(t.Context())
	if err != nil {
		t.Fatalf("appliedVersions: %v", err)
	}
	want := len(railsMigrations) + 1
	if len(versions) != want {
		t.Errorf("%d versions after two migrations, want %d", len(versions), want)
	}
}

// Go opens a database Rails made. The schema is already there, and Migrate
// does not lay anything on top of it.
func TestMigrateOnADatabaseRailsAlreadySetUp(t *testing.T) {
	db := newTestDB(t)
	loadSQL(t, db, filepath.Join("testdata", "rails_rows.sql"))

	statements := schemaStatements(t, db)

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	assertEqualStrings(t, schemaStatements(t, db), statements)

	user, err := db.UserByID(t.Context(), 1)
	if err != nil {
		t.Fatalf("the user did not survive the migration: %v", err)
	}
	if user.Email != "someone@example.com" {
		t.Errorf("email = %q", user.Email)
	}
}

func TestMigrationVersion(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"migrations/20261101093000_add_a_column.sql", "20261101093000", true},
		{"migrations/20261101093000_a.sql", "20261101093000", true},
		{"migrations/nounderscore.sql", "", false},
	}

	for _, test := range tests {
		got, ok := migrationVersion(test.name)
		if got != test.want || ok != test.wantOK {
			t.Errorf("migrationVersion(%q) = %q, %v; want %q, %v",
				test.name, got, ok, test.want, test.wantOK)
		}
	}
}

// schemaStatements is what sqlite_master holds, sorted, which is the same
// comparison `sqlite3 .schema | sort` makes.
func schemaStatements(t *testing.T, db *DB) []string {
	t.Helper()

	rows, err := db.sql.QueryContext(t.Context(),
		`SELECT sql FROM sqlite_master WHERE sql IS NOT NULL`)
	if err != nil {
		t.Fatalf("reading sqlite_master: %v", err)
	}
	defer rows.Close()

	var statements []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		statements = append(statements, statement)
	}
	slices.Sort(statements)
	return statements
}

// execCapturedSchema loads a dump shaped like testdata/rails_schema.sql: the
// comment header dropped, and every CREATE TABLE run before any CREATE
// INDEX. The dump itself is ordered by name, so an index can precede the
// table it belongs to — fine for `sqlite3 .schema`, fatal for exec, which
// runs each statement as it reaches it.
func execCapturedSchema(t *testing.T, db *DB, dump string) {
	t.Helper()

	var body []string
	for _, line := range strings.Split(dump, "\n") {
		if !strings.HasPrefix(line, "--") {
			body = append(body, line)
		}
	}

	var tables, indexes []string
	for _, statement := range strings.Split(strings.Join(body, "\n"), ";\n") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if strings.HasPrefix(statement, "CREATE TABLE") {
			tables = append(tables, statement)
		} else {
			indexes = append(indexes, statement)
		}
	}

	ordered := strings.Join(append(tables, indexes...), ";\n") + ";"
	if _, err := db.sql.ExecContext(t.Context(), ordered); err != nil {
		t.Fatalf("loading the captured schema: %v", err)
	}
}

func diffStatements(got, want []string) string {
	var report strings.Builder
	for _, statement := range want {
		if !slices.Contains(got, statement) {
			report.WriteString("missing: " + statement + "\n")
		}
	}
	for _, statement := range got {
		if !slices.Contains(want, statement) {
			report.WriteString("extra:   " + statement + "\n")
		}
	}
	return report.String()
}
