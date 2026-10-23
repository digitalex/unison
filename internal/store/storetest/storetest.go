// Package storetest provides databases for tests: a fresh SQLite file by
// default, or, when UNISON_TEST_POSTGRES holds a Postgres DSN, a private
// schema in that database (dropped when the test ends).
//
//	UNISON_TEST_POSTGRES=postgres://user:pass@localhost:5432/unison_test go test ./...
package storetest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // Registers the "pgx" driver.
)

// DSN returns a database DSN for store.Open that is empty and private to t.
func DSN(t testing.TB) string {
	t.Helper()
	pg := os.Getenv("UNISON_TEST_POSTGRES")
	if pg == "" {
		return filepath.Join(t.TempDir(), "unison.db")
	}
	admin, err := sql.Open("pgx", pg)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	rand.Read(b)
	schema := "test_" + hex.EncodeToString(b)
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatalf("creating test schema: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		admin.Close()
	})
	return withSearchPath(t, pg, schema)
}

func withSearchPath(t testing.TB, dsn, schema string) string {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return dsn + " search_path=" + schema
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
