// Package testutil provides shared test helpers for the gomajor test suite.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteModFile writes a go.mod file with the given content into dir and returns
// the absolute path to the created file. It calls t.Fatalf on any error.
func WriteModFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("testutil.WriteModFile: %v", err)
	}
	return p
}
