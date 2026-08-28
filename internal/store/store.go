// Package store is the progress database: SPEC §8's SQLite file, opened once
// at startup and written through as the learner works.
//
// It is deliberately thin. Every scheduling decision is a pure function of a
// card's state and one rating, and the review log is append-only, so the store
// never has to compute anything: it writes what the scheduler already decided
// and reads it back on the next launch. `attempts` and `reviews` are the raw
// records from which `exercises` and `cards` could be rebuilt if they were
// ever lost.
//
// The driver is modernc.org/sqlite, which is a translation of SQLite into Go
// rather than a binding, so `bt` stays a cgo-free single binary.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

// Store is an open progress database.
type Store struct {
	db   *sql.DB
	path string
}

// Memory is the path that opens a private in-memory database, which is what
// tests and `--no-store` runs use: every method works, nothing outlives the
// process.
const Memory = ":memory:"

// Open opens the database at path, creating the file and its parent directory
// if they do not exist, and migrates it to the current schema.
func Open(path string) (*Store, error) {
	dsn := path
	if path != Memory {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("progress directory: %w", err)
		}
		dsn = "file:" + path
	}
	// busy_timeout keeps a second `bt` waiting rather than failing outright;
	// WAL keeps a read during a write from blocking at all.
	dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection: this is a single-user TUI, and serialising writes here
	// is cheaper than reasoning about SQLITE_BUSY everywhere else.
	db.SetMaxOpenConns(1)

	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// OpenMemory opens a private in-memory database.
func OpenMemory() (*Store, error) { return Open(Memory) }

// Close releases the database.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return s.db.Close()
}

// Path reports where the database lives, for `bt doctor`.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// migration is one forward-only schema step. Steps are never edited once
// released and never run backwards: a new column means a new migration, so a
// database written by an older `bt` is always reachable from here.
type migration struct {
	version int
	stmts   []string
}

// migrations are applied in order; the database's user_version records how far
// it has got. It matches len(migrations) on an up-to-date file.
//
// The `cards` and `exercises` tables carry two columns each beyond SPEC §8's
// sketch — cards.first_seen, which the daily new-card cap is counted against,
// and exercises.hints/solution_shown, which restore what the learner had
// already uncovered of an exercise.
var migrations = []migration{{
	version: 1,
	stmts: []string{
		`CREATE TABLE cards (
			id          TEXT PRIMARY KEY,
			stability   REAL    NOT NULL,
			difficulty  REAL    NOT NULL,
			due         INTEGER NOT NULL,
			reps        INTEGER NOT NULL,
			lapses      INTEGER NOT NULL,
			last_review INTEGER NOT NULL,
			first_seen  INTEGER NOT NULL
		)`,
		`CREATE TABLE reviews (
			id         INTEGER PRIMARY KEY,
			card_id    TEXT    NOT NULL,
			ts         INTEGER NOT NULL,
			rating     INTEGER NOT NULL,
			elapsed_ms INTEGER NOT NULL,
			source     TEXT    NOT NULL
		)`,
		`CREATE INDEX reviews_by_card ON reviews(card_id)`,
		`CREATE INDEX reviews_by_ts ON reviews(ts)`,
		`CREATE TABLE attempts (
			id          INTEGER PRIMARY KEY,
			exercise_id TEXT    NOT NULL,
			ts          INTEGER NOT NULL,
			input       TEXT    NOT NULL,
			passed      INTEGER NOT NULL,
			hints_used  INTEGER NOT NULL,
			ms          INTEGER NOT NULL
		)`,
		`CREATE INDEX attempts_by_exercise ON attempts(exercise_id)`,
		`CREATE TABLE exercises (
			id             TEXT PRIMARY KEY,
			first_passed   INTEGER NOT NULL,
			best_ms        INTEGER NOT NULL,
			attempts       INTEGER NOT NULL,
			hints          INTEGER NOT NULL,
			solution_shown INTEGER NOT NULL
		)`,
		`CREATE TABLE meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	},
}}

// SchemaVersion is the schema this build writes.
var SchemaVersion = len(migrations)

// ErrNewerSchema is returned when the file on disk was written by a newer
// build. Migrations are forward-only, so there is nothing to do but say so.
var ErrNewerSchema = errors.New("progress database was written by a newer bash-teacher")

// migrate brings the database up to SchemaVersion, one numbered step at a
// time, each in its own transaction so a failure leaves the version behind it
// intact.
func (s *Store) migrate() error {
	var have int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&have); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if have > SchemaVersion {
		return fmt.Errorf("%w (schema %d, this build knows %d)", ErrNewerSchema, have, SchemaVersion)
	}
	for _, m := range migrations {
		if m.version <= have {
			continue
		}
		if err := s.apply(m); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
	}
	return nil
}

// apply runs one migration in a transaction. A failure rolls back and reports
// both errors: a rollback that also fails leaves the file in a state worth
// hearing about, and hiding it behind the error that caused it would make that
// invisible.
func (s *Store) apply(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range m.stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return errors.Join(err, tx.Rollback())
		}
	}
	// PRAGMA does not take a placeholder; the value is an int from a literal
	// in this file, never from input.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// Meta reads one metadata value, reporting whether it was set.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetMeta writes one metadata value.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// unix flattens a time for storage. The zero time is stored as 0 rather than
// as a negative epoch offset, so "never" reads the same on the way back.
func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// at is unix's inverse. Times come back in the local zone because the daily
// caps and the streak are about the learner's day, not the clock's.
func at(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}
