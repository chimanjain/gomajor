package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeModFile is a test helper that writes a go.mod file to dir and returns its path.
func writeModFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeModFile: %v", err)
	}
	return p
}

func TestResolveModFile(t *testing.T) {
	tests := []struct {
		name      string
		createMod bool
		wantErr   bool
	}{
		{
			name:      "FindsInCwd",
			createMod: true,
			wantErr:   false,
		},
		{
			name:      "ErrorWhenMissing",
			createMod: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			if tt.createMod {
				writeModFile(t, dir, "module example.com/test\n\ngo 1.21\n")
			}

			orig, err := os.Getwd()
			if err != nil {
				t.Fatalf("os.Getwd: %v", err)
			}
			defer os.Chdir(orig) //nolint:errcheck

			if err := os.Chdir(dir); err != nil {
				t.Fatalf("os.Chdir: %v", err)
			}

			got, err := resolveModFile()
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveModFile() returned error = %v, wantErr = %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				wantPath := filepath.Join(dir, "go.mod")
				if got != wantPath {
					t.Errorf("resolveModFile() = %q, want %q", got, wantPath)
				}
			}
		})
	}
}

func TestRootCmd(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		parseOnly bool
		checkFn   func(t *testing.T, gotOutput string)
	}{
		{
			name:      "Flags_Defaults",
			parseOnly: true,
			checkFn: func(t *testing.T, _ string) {
				if config.MaxProbe != 5 {
					t.Errorf("MaxProbe = %v, want 5", config.MaxProbe)
				}
			},
		},
		{
			name:      "Flags_Github_SingleRepo",
			args:      []string{"-g", "owner/repo"},
			parseOnly: true,
			checkFn: func(t *testing.T, _ string) {
				want := []string{"owner/repo"}
				if !slices.Equal(config.GitHubRepos, want) {
					t.Errorf("GitHubRepos = %v, want %v", config.GitHubRepos, want)
				}
			},
		},
		{
			name:      "Flags_Github_CommaSeparated",
			args:      []string{"-g", "owner/repo1,github.com/owner/repo2"},
			parseOnly: true,
			checkFn: func(t *testing.T, _ string) {
				want := []string{"owner/repo1", "github.com/owner/repo2"}
				if !slices.Equal(config.GitHubRepos, want) {
					t.Errorf("GitHubRepos = %v, want %v", config.GitHubRepos, want)
				}
			},
		},
		{
			name: "Version",
			args: []string{"--version"},
			checkFn: func(t *testing.T, got string) {
				if got != "v1.6.0\n" {
					t.Errorf("got %q, want v1.6.0\n", got)
				}
			},
		},
		{
			name: "Help_Long",
			args: []string{"--help"},
			checkFn: func(t *testing.T, got string) {
				if !strings.Contains(got, "Checks for major version updates") || !strings.Contains(got, "Usage:") {
					t.Errorf("expected help output, got: %q", got)
				}
			},
		},
		{
			name: "Help_Short",
			args: []string{"-h"},
			checkFn: func(t *testing.T, got string) {
				if !strings.Contains(got, "Checks for major version updates") || !strings.Contains(got, "Usage:") {
					t.Errorf("expected help output, got: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config = DefaultConfig()
			if err := rootCmd.Flags().Set("github", ""); err != nil {
				t.Fatalf("failed to reset github flag: %v", err)
			}

			var buf bytes.Buffer
			rootCmd.SetOut(&buf)
			rootCmd.SetErr(&buf)
			defer rootCmd.SetOut(nil)
			defer rootCmd.SetErr(nil)
			defer rootCmd.SetArgs(nil)

			var err error
			if tt.parseOnly {
				err = rootCmd.ParseFlags(tt.args)
			} else {
				rootCmd.SetArgs(tt.args)
				err = rootCmd.Execute()
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkFn != nil {
				tt.checkFn(t, buf.String())
			}
		})
	}
	config.GitHubRepos = nil
}
