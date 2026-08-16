package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

//go:embed migrations
var migrationFiles embed.FS

// railsMigrations is every version in db/migrate, oldest first. Rails records
// these in schema_migrations as it runs them and refuses to run one twice; a
// database that Go created has to carry the same list, or a `kamal rollback`
// to the Rails image would find an empty schema_migrations, believe none of
// the migrations had run, and try to create the tables again.
//
// They are the timestamps in the db/migrate filenames. That directory goes
// away with the rest of Rails at the end of the rewrite, which is why the list
// is copied here rather than derived from it.
var railsMigrations = []string{
	"20260806210000", // create_users
	"20260806210100", // create_sessions
	"20260807100000", // create_start_pages
	"20260807100100", // create_start_page_groups
	"20260807100200", // create_start_page_items
	"20260807140000", // create_tinylinks_connections
	"20260807170000", // scope_tinylinks_connections_to_users
	"20260807180000", // move_start_pages_into_users
	"20260807190000", // default_users_to_one_column
	"20260807210000", // drop_gray_from_the_palette
	"20260809120000", // rename_tinylinks_connections_to_connections
}

// Migrate brings the database up to date, and is safe to call on every boot.
//
// On a database that already has the tables — which is every real one, since
// production has been running Rails — it applies nothing but the pending files
// in migrations/, of which there are none today. On an empty file it lays down
// schema.sql, writes the eleven Rails versions into schema_migrations and the
// environment row into ar_internal_metadata, so that the result is
// indistinguishable from a database Rails set up itself.
func (db *DB) Migrate(ctx context.Context) error {
	installed, err := db.schemaInstalled(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if !installed {
		if err := db.installSchema(ctx); err != nil {
			return fmt.Errorf("migrate: installing the schema: %w", err)
		}
	}

	if err := db.applyPendingMigrations(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// schemaInstalled asks whether this is a database or an empty file. users is
// the anchor: it is the table every other one points at, and it has existed
// since the first migration.
//
// The check cannot be skipped by making schema.sql re-runnable. Rails wrote
// its CREATE TABLE statements with IF NOT EXISTS and its CREATE INDEX
// statements without, and SQLite stores the statement text verbatim in
// sqlite_master — so adding IF NOT EXISTS to the indexes would leave a
// database that no longer matches the one Rails produces.
func (db *DB) schemaInstalled(ctx context.Context) (bool, error) {
	var name string
	err := db.sql.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// installSchema is the fresh-database path, in one transaction: either the
// file comes out of this with a complete schema or it comes out of it empty.
func (db *DB) installSchema(ctx context.Context) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
			return err
		}

		for _, version := range railsMigrations {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
				return err
			}
		}

		// Rails writes two rows here: this one and a schema_sha1 it uses to
		// decide whether db/schema.rb is stale. Only the environment matters
		// to a running app — it is what makes `rails db:drop` refuse in
		// production — and this app has exactly one environment.
		now := utcNow()
		_, err := tx.ExecContext(ctx,
			`INSERT INTO ar_internal_metadata (key, value, created_at, updated_at)
			 VALUES ('environment', 'production', ?, ?)`,
			railsTime(now), railsTime(now))
		return err
	})
}

// applyPendingMigrations runs every file in migrations/ whose version is not
// already recorded, in filename order. There are none yet; migrations/README.md
// describes the convention for adding one.
func (db *DB) applyPendingMigrations(ctx context.Context) error {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)

	applied, err := db.appliedVersions(ctx)
	if err != nil {
		return err
	}

	for _, name := range names {
		version, ok := migrationVersion(name)
		if !ok {
			return fmt.Errorf("%s is not named <version>_<name>.sql", name)
		}
		if slices.Contains(applied, version) {
			continue
		}

		statements, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			return err
		}

		// One transaction per file, and the version is recorded inside it: a
		// migration that fails half way leaves nothing behind, so the next
		// boot runs it again from the start.
		err = db.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(statements)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (version) VALUES (?)`, version)
			return err
		})
		if err != nil {
			return fmt.Errorf("applying %s: %w", name, err)
		}
	}
	return nil
}

// appliedVersions is what schema_migrations holds, whoever wrote it — Rails
// rows and Go rows are the same rows.
func (db *DB) appliedVersions(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

// migrationVersion pulls the timestamp off the front of a filename:
// "migrations/20261101093000_add_a_column.sql" is version 20261101093000.
func migrationVersion(name string) (string, bool) {
	base := name[strings.LastIndex(name, "/")+1:]
	version, _, found := strings.Cut(base, "_")
	if !found || version == "" {
		return "", false
	}
	return version, true
}
