//go:build integration

package postgres_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// migrationsDir resolves /migrations relative to this test file, so the
// test works regardless of the working directory `go test` is invoked
// from.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

func requireDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres integration test")
	}
	return url
}
