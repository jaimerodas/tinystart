// Package store is the only part of TinyStart that knows SQL. Everything above
// it — the HTTP handlers, the templates, the import and export services — asks
// for records and gets Go structs back, so a change to a query is a change to
// one package.
//
// The database is the one the Rails app has been writing since 2026 and can be
// handed back to it at any time, which is why the schema is Rails' schema, the
// timestamps are in Rails' format (see time.go) and the validation messages are
// word for word ActiveRecord's (see errors.go).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"             // registers the "sqlite" driver, and names its error type
	sqlite3 "modernc.org/sqlite/lib" // the SQLITE_* result codes
)

// DB is a handle on the database. The *sql.DB inside is unexported on purpose:
// if it were embedded, any package could reach through it and run a query, and
// the one-package-knows-SQL rule would last about a week.
type DB struct {
	sql *sql.DB
}

// Open connects to the SQLite file at path, creating it if it is not there,
// and verifies the connection before returning. It does not create any tables;
// that is Migrate.
func Open(ctx context.Context, path string) (*DB, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	// One connection, which is to say: every statement this process runs
	// against SQLite is serialised, and SQLITE_BUSY between our own goroutines
	// cannot happen at all.
	//
	// The alternative — a pool of readers plus a single writer — buys read
	// concurrency, which an app with one user and a page that issues about
	// four queries has no use for.
	//
	// It does mean that a query run on db while a transaction is open on db
	// waits forever for the connection the transaction is holding. That is a
	// deadlock every time rather than under load, which is the better of the
	// two: it shows up on the first run instead of on a bad Tuesday. No method
	// here does it — everything a transaction needs, it asks the *sql.Tx for.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	return &DB{sql: db}, nil
}

// dsn spells out the settings that have to hold on every connection.
//
// journal_mode is stored in the file itself and so only takes on the first
// connection, but the other two are per-connection and would be silently lost
// on any connection opened later — which is exactly the bug that makes foreign
// keys "work in tests and not in production". Setting them here means the
// driver applies them to every connection it opens, rather than us running
// PRAGMA on one of them and hoping.
//
//   - journal_mode=wal: readers do not block the writer, and the backup script
//     can copy the file while the app is running.
//   - busy_timeout=5000: another process holding the write lock — the old
//     container during a deploy, bin/backup_db — is waited out rather than
//     reported. Five seconds is far longer than any write here takes.
//   - foreign_keys=on: SQLite ignores the REFERENCES clauses in the schema
//     unless asked not to, and the schema is Rails' schema precisely so that
//     both images enforce the same shape.
//
// _txlock=immediate makes every transaction take the write lock at BEGIN. The
// transactions in this package all read and then write, and a deferred
// transaction that upgrades from a read lock to a write lock mid-way is the
// one kind of contention busy_timeout does not retry: SQLite returns
// SQLITE_BUSY immediately, because rolling back is the only way out. Taking
// the lock up front costs nothing here and cannot deadlock.
func dsn(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(wal)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(on)" +
		"&_txlock=immediate"
}

// Close releases the connection. Only main calls it.
func (db *DB) Close() error { return db.sql.Close() }

// tx runs fn inside a transaction, committing if it returns nil and rolling
// back otherwise.
//
// The deferred Rollback is the whole trick: it runs on the error path, on the
// panic path, and after a successful Commit, where it is a no-op that returns
// sql.ErrTxDone. That is why the error is dropped there and nowhere else — the
// alternative is remembering to roll back at each of the half-dozen returns
// inside a move, and forgetting once leaves the database locked.
func (db *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return conflict(err)
	}
	return conflict(tx.Commit())
}

// conflict turns a unique-index violation into ErrConflict.
//
// Every uniqueness rule in this package is checked before the write, inside
// the same transaction, and reported as the ValidationError the form shows —
// so reaching here means the index caught something the check could not: two
// requests inserting the same row at the same instant, or a row that predates
// a rule. There is no field to blame and nothing sensible to say on a form,
// which is exactly what a distinct sentinel is for.
func conflict(err error) error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return fmt.Errorf("%w: %v", ErrConflict, err)
		}
	}
	return err
}

// notFound turns database/sql's sentinel into ours, so that no caller outside
// this package has to know which database library is underneath. Any other
// error passes through untouched.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// scanner is the little that *sql.Row and *sql.Rows have in common, which is
// all a scanX function needs — and is what lets one of them serve both a
// single-row lookup and a loop over many.
type scanner interface {
	Scan(dest ...any) error
}

// querier is the overlap between *sql.DB and *sql.Tx, so the small helpers
// below can be used inside a transaction and outside one.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// exists answers a `SELECT 1 …` without the caller having to spell out the
// ErrNoRows dance every time. It is the shape every uniqueness check and every
// "is there anybody at all" question takes.
func exists(ctx context.Context, q querier, query string, args ...any) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, query+" LIMIT 1", args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// update runs a statement that must match exactly one row, and reports
// ErrNotFound when it matched none. Without that check an UPDATE against an id
// that has since been deleted looks like a success and the page redraws
// something that is no longer there.
func (db *DB) update(ctx context.Context, query string, args ...any) error {
	result, err := db.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return conflict(err)
	}
	return mustHaveChanged(result)
}

func mustHaveChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}
