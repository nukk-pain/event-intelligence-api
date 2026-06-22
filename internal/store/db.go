// Package store provides the SQLite-backed canonical event store.
//
// It uses the pure-Go driver modernc.org/sqlite (driver name "sqlite") so the
// build stays CGo-free and ships as a single static binary.
//
// Concurrency model: one writer process (single connection) plus many reader
// connections, all sharing one SQLite file in WAL mode. PRAGMAs are applied on
// every connection through the DSN so the database/sql pool cannot hand out a
// connection without them.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // driver name: "sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// writePragmaQuery holds PRAGMA directives applied on write connections. The
// modernc.org/sqlite driver accepts them as repeated _pragma DSN parameters.
var writePragmaQuery = url.Values{
	"_pragma": []string{
		"journal_mode(WAL)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
		"foreign_keys(ON)",
	},
}

// readPragmaQuery avoids journal_mode(WAL) because changing journal mode writes
// SQLite metadata and fails on mode=ro handles. The writer is responsible for
// setting WAL; readers only apply connection-local safety settings.
var readPragmaQuery = url.Values{
	"_pragma": []string{
		"busy_timeout(5000)",
		"foreign_keys(ON)",
	},
}

// OpenWrite opens the database for writing. The pool is capped at a single
// connection to enforce the single-writer invariant under WAL.
func OpenWrite(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?%s", path, writePragmaQuery.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping write db: %w", err)
	}
	return db, nil
}

// OpenRead opens the database read-only via the file: DSN with mode=ro. Writes
// on this handle fail at the SQLite layer.
func OpenRead(path string) (*sql.DB, error) {
	q := url.Values{}
	for k, vs := range readPragmaQuery {
		q[k] = append([]string(nil), vs...)
	}
	q.Set("mode", "ro")
	dsn := fmt.Sprintf("file:%s?%s", path, q.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open read db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping read db: %w", err)
	}
	return db, nil
}

// Migrate applies EVERY embedded migration under migrations/, in ascending
// filename order (0001_…, 0002_…, …). Each migration is idempotent (CREATE …
// IF NOT EXISTS), so calling Migrate on every writer startup is safe and runs
// the whole set, not just the first file.
func Migrate(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // filename order is the migration order (0001 < 0002 …)

	for _, name := range names {
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read embedded migration %s: %w", name, err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			if isAlreadyAppliedMigration(err) {
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func isAlreadyAppliedMigration(err error) bool {
	return strings.Contains(err.Error(), "duplicate column name: summary")
}
