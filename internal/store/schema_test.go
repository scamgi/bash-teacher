package store_test

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

// bumpSchema forges a database written by a newer build, which is the one
// state the store has to refuse and cannot reach by itself.
func bumpSchema(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
}
