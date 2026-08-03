package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/internal/testutil"
	"github.com/spf13/cobra"
)

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
				testutil.WriteModFile(t, dir, "module example.com/test\n\ngo 1.21\n")
			}

			t.Chdir(dir)

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
		checkFn   func(t *testing.T, cmd *cobra.Command, gotOutput string)
	}{
		{
			name:      "Flags_Defaults",
			parseOnly: true,
			checkFn: func(t *testing.T, cmd *cobra.Command, _ string) {
				cfg, _, err := parseConfig(cmd)
				if err != nil {
					t.Fatalf("parseConfig: %v", err)
				}
				if cfg.MaxProbe != 5 {
					t.Errorf("MaxProbe = %v, want 5", cfg.MaxProbe)
				}
			},
		},
		{
			name:      "Flags_Github_SingleRepo",
			args:      []string{"-g", "owner/repo"},
			parseOnly: true,
			checkFn: func(t *testing.T, cmd *cobra.Command, _ string) {
				cfg, _, err := parseConfig(cmd)
				if err != nil {
					t.Fatalf("parseConfig: %v", err)
				}
				want := []string{"owner/repo"}
				if !slices.Equal(cfg.GitHubRepos, want) {
					t.Errorf("GitHubRepos = %v, want %v", cfg.GitHubRepos, want)
				}
			},
		},
		{
			name:      "Flags_Github_CommaSeparated",
			args:      []string{"-g", "owner/repo1,github.com/owner/repo2"},
			parseOnly: true,
			checkFn: func(t *testing.T, cmd *cobra.Command, _ string) {
				cfg, _, err := parseConfig(cmd)
				if err != nil {
					t.Fatalf("parseConfig: %v", err)
				}
				want := []string{"owner/repo1", "github.com/owner/repo2"}
				if !slices.Equal(cfg.GitHubRepos, want) {
					t.Errorf("GitHubRepos = %v, want %v", cfg.GitHubRepos, want)
				}
			},
		},
		{
			name: "Version",
			args: []string{"--version"},
			checkFn: func(t *testing.T, _ *cobra.Command, got string) {
				if got != "v1.8.0\n" {
					t.Errorf("got %q, want v1.8.0\n", got)
				}
			},
		},
		{
			name: "Help_Long",
			args: []string{"--help"},
			checkFn: func(t *testing.T, _ *cobra.Command, got string) {
				if !strings.Contains(got, "A tool that parses a go.mod file") || !strings.Contains(got, "Usage:") {
					t.Errorf("expected help output, got: %q", got)
				}
			},
		},
		{
			name: "Help_Short",
			args: []string{"-h"},
			checkFn: func(t *testing.T, _ *cobra.Command, got string) {
				if !strings.Contains(got, "A tool that parses a go.mod file") || !strings.Contains(got, "Usage:") {
					t.Errorf("expected help output, got: %q", got)
				}
			},
		},
		{
			name:      "Flags_Verbose",
			args:      []string{"--verbose"},
			parseOnly: true,
			checkFn: func(t *testing.T, cmd *cobra.Command, _ string) {
				cfg, _, err := parseConfig(cmd)
				if err != nil {
					t.Fatalf("parseConfig: %v", err)
				}
				if !cfg.Verbose {
					t.Errorf("Verbose = %v, want true", cfg.Verbose)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCmd()

			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)

			var err error
			if tt.parseOnly {
				err = cmd.ParseFlags(tt.args)
			} else {
				cmd.SetArgs(tt.args)
				err = cmd.Execute()
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkFn != nil {
				tt.checkFn(t, cmd, buf.String())
			}
		})
	}
}

func TestConfigMerging(t *testing.T) {
	t.Run("YAMLConfigOnly", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		configContent := `
minor: false
major: false
output: my-custom-report.json
`
		if err := os.WriteFile("gomajor.yaml", []byte(configContent), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		cmd := NewRootCmd()
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		cfg, _, err := parseConfig(cmd)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}

		if cfg.Minor {
			t.Errorf("expected Minor to be false (from YAML), got true")
		}
		if cfg.Major {
			t.Errorf("expected Major to be false (from YAML), got true")
		}
		if cfg.OutputPath != "my-custom-report.json" {
			t.Errorf("expected OutputPath to be my-custom-report.json (from YAML), got %s", cfg.OutputPath)
		}
	})

	t.Run("CLIOverridesYAML", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		configContent := `
minor: false
major: false
output: my-custom-report.json
`
		if err := os.WriteFile("gomajor.yaml", []byte(configContent), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		cmd := NewRootCmd()
		// Override minor and output in CLI flags
		args := []string{"--minor=true", "--output=override.json"}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		cfg, _, err := parseConfig(cmd)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}

		if !cfg.Minor {
			t.Errorf("expected Minor to be true (CLI override), got false")
		}
		// major is not in args, so it should still be false from YAML
		if cfg.Major {
			t.Errorf("expected Major to be false (from YAML), got true")
		}
		if cfg.OutputPath != "override.json" {
			t.Errorf("expected OutputPath to be override.json (CLI override), got %s", cfg.OutputPath)
		}
	})
}
