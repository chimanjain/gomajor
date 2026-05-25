package cmd

import (
	"os"
	"path/filepath"
	"testing"
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
	if config.MaxProbe != 5 {
		t.Errorf("default MaxProbe = %d, want 5", config.MaxProbe)
	}
	if config.CheckAll {
		t.Errorf("default CheckAll = true, want false")
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

func TestExecute(t *testing.T) {
	dir := t.TempDir()
	content := "module example.com/test\n\ngo 1.21\n"
	p := writeModFile(t, dir, content)

	// Set the flag via command line args for rootCmd
	os.Args = []string{"gomajor", "--file", p}

	// Execute calls rootCmd.Execute() which calls runChecker.
	Execute()
}

func TestRootCmd_NoColorFlag(t *testing.T) {
	if config.NoColor {
		t.Errorf("default NoColor = true, want false")
	}
}
