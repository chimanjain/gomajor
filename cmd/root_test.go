package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chimanjain/gomajor/checker"
)

func TestResolveModFile_FindsInCwd(t *testing.T) {
	// Create a temporary directory with a go.mod file.
	dir := t.TempDir()
	goModPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("failed to create temp go.mod: %v", err)
	}

	// Change into that directory so resolveModFile() can find it.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}

	got, err := resolveModFile()
	if err != nil {
		t.Fatalf("resolveModFile() returned unexpected error: %v", err)
	}
	if got != goModPath {
		t.Errorf("resolveModFile() = %q, want %q", got, goModPath)
	}
}

func TestResolveModFile_ErrorWhenMissing(t *testing.T) {
	// Switch to a temp directory that has NO go.mod.
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}

	_, err = resolveModFile()
	if err == nil {
		t.Error("resolveModFile() expected an error when go.mod is absent, got nil")
	}
}

func TestRootCmd_DefaultFlags(t *testing.T) {
	// maxProbe default is 5, checkAll default is false.
	if maxProbe != 5 {
		t.Errorf("default maxProbe = %d, want 5", maxProbe)
	}
	if checkAll {
		t.Errorf("default checkAll = true, want false")
	}
}

// writeModFile is a test helper that writes a go.mod file to dir and returns its path.
func writeModFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("writeModFile: %v", err)
	}
	return p
}

func TestRunChecker_NoDirectDeps(t *testing.T) {
	dir := t.TempDir()
	// Only indirect dependencies — runChecker should print "No matching dependencies".
	content := `module example.com/test

go 1.21

require github.com/google/uuid v1.6.0 // indirect
`
	p := writeModFile(t, dir, content)
	modFilePath = p
	checkAll = false
	maxProbe = 0 // no network probing

	// runChecker must not panic; we just verify it returns without crashing.
	// (Output goes to stdout which is harmless in tests.)
	defer func() {
		modFilePath = ""
		checkAll = false
		maxProbe = 5
	}()

	// We can't call runChecker(true) directly in a test because it calls os.Exit
	// on errors, but with a valid file and fileExplicit=true it should run cleanly.
	runChecker(true)
}

func TestRunChecker_EmptyMod(t *testing.T) {
	dir := t.TempDir()
	content := "module example.com/empty\n\ngo 1.21\n"
	p := writeModFile(t, dir, content)
	modFilePath = p
	checkAll = false
	maxProbe = 0

	defer func() {
		modFilePath = ""
		checkAll = false
		maxProbe = 5
	}()

	runChecker(true)
}

func TestRunChecker_WithUpdatesMock(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/test

go 1.21

require github.com/foo/bar v1.0.0
`
	p := writeModFile(t, dir, content)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		} else {
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	originalProxyBase := checker.ProxyBase
	checker.ProxyBase = server.URL
	defer func() { checker.ProxyBase = originalProxyBase }()

	modFilePath = p
	checkAll = false
	maxProbe = 2

	defer func() {
		modFilePath = ""
		checkAll = false
		maxProbe = 5
	}()

	runChecker(true)
}

func TestRunChecker_AllDeps(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/test

go 1.21

require (
	github.com/foo/bar v1.0.0
	github.com/foo/baz v1.0.0 // indirect
)
`
	p := writeModFile(t, dir, content)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	originalProxyBase := checker.ProxyBase
	checker.ProxyBase = server.URL
	defer func() { checker.ProxyBase = originalProxyBase }()

	modFilePath = p
	checkAll = true
	maxProbe = 1

	defer func() {
		modFilePath = ""
		checkAll = false
		maxProbe = 5
	}()

	runChecker(true)
}

func TestExecute(t *testing.T) {
	dir := t.TempDir()
	content := "module example.com/test\n\ngo 1.21\n"
	p := writeModFile(t, dir, content)

	// Set the flag via command line args for rootCmd
	os.Args = []string{"gomajor", "--file", p}

	// Execute calls rootCmd.Execute() which calls runChecker.
	Execute()
}
