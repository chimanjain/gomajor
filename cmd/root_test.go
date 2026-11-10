package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/internal/testutil"
	"github.com/chimanjain/gomajor/pkg/config"
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

func parseTestCmd(t *testing.T, args []string) *config.Config {
	t.Helper()
	cmd := NewRootCmd()
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	cfg, err := parseConfig(cmd)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	return cfg
}

func TestRootCmd(t *testing.T) {
	t.Run("Flags", func(t *testing.T) {
		tests := []struct {
			name  string
			args  []string
			check func(t *testing.T, cfg *config.Config)
		}{
			{"Defaults", nil, func(t *testing.T, cfg *config.Config) {
				if cfg.MaxProbe != 5 {
					t.Errorf("MaxProbe = %v, want 5", cfg.MaxProbe)
				}
			}},
			{"MaxProbe_Capped", []string{"-m", "999"}, func(t *testing.T, cfg *config.Config) {
				if cfg.MaxProbe != 50 {
					t.Errorf("MaxProbe = %v, want 50 (capped)", cfg.MaxProbe)
				}
			}},
			{"Github_SingleRepo", []string{"-g", "owner/repo"}, func(t *testing.T, cfg *config.Config) {
				if !slices.Equal(cfg.GitHubRepos, []string{"owner/repo"}) {
					t.Errorf("GitHubRepos = %v", cfg.GitHubRepos)
				}
			}},
			{"Github_CommaSeparated", []string{"-g", "owner/repo1,github.com/owner/repo2"}, func(t *testing.T, cfg *config.Config) {
				if !slices.Equal(cfg.GitHubRepos, []string{"owner/repo1", "github.com/owner/repo2"}) {
					t.Errorf("GitHubRepos = %v", cfg.GitHubRepos)
				}
			}},
			{"Verbose", []string{"--verbose"}, func(t *testing.T, cfg *config.Config) {
				if !cfg.Verbose {
					t.Errorf("Verbose = %v, want true", cfg.Verbose)
				}
			}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := parseTestCmd(t, tt.args)
				tt.check(t, cfg)
			})
		}
	})

	t.Run("Version", func(t *testing.T) {
		cmd := NewRootCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--version"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if buf.String() != Version+"\n" {
			t.Errorf("got %q, want %q", buf.String(), Version+"\n")
		}
	})

	t.Run("Help", func(t *testing.T) {
		cmd := NewRootCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.Contains(got, "A tool that parses a go.mod file") || !strings.Contains(got, "Usage:") {
			t.Errorf("expected help output, got: %q", got)
		}
	})
}

func TestConfigMerging(t *testing.T) {
	t.Run("YAMLConfigOnly", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		configContent := `
minor: false
major: false
output: my-custom-report.json
all: true
max_probe: 12
json: true
no_color: true
verbose: true
`
		if err := os.WriteFile("gomajor.yaml", []byte(configContent), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		cmd := NewRootCmd()
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		cfg, err := parseConfig(cmd)
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
		if !cfg.CheckAll {
			t.Errorf("expected CheckAll to be true (from YAML), got false")
		}
		if cfg.MaxProbe != 12 {
			t.Errorf("expected MaxProbe to be 12 (from YAML), got %d", cfg.MaxProbe)
		}
		if !cfg.JSONOutput {
			t.Errorf("expected JSONOutput to be true (from YAML), got false")
		}
		if !cfg.NoColor {
			t.Errorf("expected NoColor to be true (from YAML), got false")
		}
		if !cfg.Verbose {
			t.Errorf("expected Verbose to be true (from YAML), got false")
		}
	})

	t.Run("CLIOverridesYAML", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		configContent := `
minor: false
major: false
output: my-custom-report.json
all: true
max_probe: 12
no_color: true
`
		if err := os.WriteFile("gomajor.yaml", []byte(configContent), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		cmd := NewRootCmd()
		// Override minor, output, all, max-probe, and no-color in CLI flags
		args := []string{"--minor=true", "--output=override.json", "--all=false", "--max-probe=7", "--no-color=false"}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		cfg, err := parseConfig(cmd)
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
		if cfg.CheckAll {
			t.Errorf("expected CheckAll to be false (CLI override), got true")
		}
		if cfg.MaxProbe != 7 {
			t.Errorf("expected MaxProbe to be 7 (CLI override), got %d", cfg.MaxProbe)
		}
		if cfg.NoColor {
			t.Errorf("expected NoColor to be false (CLI override), got true")
		}
	})

	t.Run("BothMinorAndMajorDisabled_Error", func(t *testing.T) {
		dir := t.TempDir()
		testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\n")
		t.Chdir(dir)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"--minor=false", "--major=false"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error when both minor and major are disabled, got nil")
		}
		if !strings.Contains(err.Error(), "cannot disable both minor and major checks") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("FileFlagOverridesAutoConfig", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		configContent := `
local:
  - other/go.mod
`
		if err := os.WriteFile("gomajor.yaml", []byte(configContent), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		cmd := NewRootCmd()
		args := []string{"-f", "custom/go.mod"}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		cfg, err := parseConfig(cmd)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}

		if cfg.ConfigPath != "" {
			t.Errorf("expected ConfigPath to be empty when -f is explicitly passed, got %q", cfg.ConfigPath)
		}
		if cfg.ModFilePath != "custom/go.mod" {
			t.Errorf("expected ModFilePath to be custom/go.mod, got %q", cfg.ModFilePath)
		}
		if !isSingleMode(cfg) {
			t.Errorf("expected isSingleMode to be true when -f is passed without explicit -c")
		}
	})

	t.Run("ExplicitNonExistentConfig_Error", func(t *testing.T) {
		cmd := NewRootCmd()
		args := []string{"-c", "does-not-exist.yaml"}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		_, err := parseConfig(cmd)
		if err == nil {
			t.Error("expected error when explicit config file does not exist, got nil")
		}
	})

	t.Run("InvalidYAMLContent_Error", func(t *testing.T) {
		dir := t.TempDir()
		badConfig := filepath.Join(dir, "bad.yaml")
		if err := os.WriteFile(badConfig, []byte("local: [unclosed list"), 0o600); err != nil {
			t.Fatal(err)
		}

		cmd := NewRootCmd()
		args := []string{"-c", badConfig}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		_, err := parseConfig(cmd)
		if err == nil {
			t.Error("expected error for malformed YAML config, got nil")
		}
	})

	t.Run("OutputFlag_DefaultNames", func(t *testing.T) {
		// Output flag passed without value (empty string) with json = true
		cmdJSON := NewRootCmd()
		if err := cmdJSON.ParseFlags([]string{"-o", "", "--json"}); err != nil {
			t.Fatal(err)
		}
		cfgJSON, err := parseConfig(cmdJSON)
		if err != nil {
			t.Fatal(err)
		}
		if cfgJSON.OutputPath != "gomajor-report.json" {
			t.Errorf("expected gomajor-report.json, got %s", cfgJSON.OutputPath)
		}

		// Output flag passed without value with json = false
		cmdYAML := NewRootCmd()
		if err := cmdYAML.ParseFlags([]string{"-o", ""}); err != nil {
			t.Fatal(err)
		}
		cfgYAML, err := parseConfig(cmdYAML)
		if err != nil {
			t.Fatal(err)
		}
		if cfgYAML.OutputPath != "gomajor-report.yaml" {
			t.Errorf("expected gomajor-report.yaml, got %s", cfgYAML.OutputPath)
		}
	})
}

func TestValidateConfigOutputPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"ValidRelative", "report.json", false},
		{"ValidSubdir", "subdir/report.yaml", false},
		{"ValidDotSlash", "./reports/out.json", false},
		{"Absolute", "/etc/report.json", true},
		{"ParentTraversal", "../report.json", true},
		{"DeepTraversal", "a/../../report.json", true},
		{"DotDotDirect", "..", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigOutputPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfigOutputPath(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestIsSingleMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "SingleLocal",
			cfg:  &config.Config{},
			want: true,
		},
		{
			name: "HasConfigPath",
			cfg:  &config.Config{ConfigPath: "gomajor.yaml"},
			want: false,
		},
		{
			name: "HasCliGithub",
			cfg:  &config.Config{GitHubRepos: []string{"owner/repo"}},
			want: false,
		},
		{
			name: "HasLocalMods",
			cfg:  &config.Config{LocalMods: []string{"sub/go.mod"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSingleMode(tt.cfg)
			if got != tt.want {
				t.Errorf("isSingleMode() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRootCmd_SilenceErrors(t *testing.T) {
	cmd := NewRootCmd()
	if !cmd.SilenceErrors {
		t.Errorf("NewRootCmd() SilenceErrors = false, want true to prevent unsanitized error leaking")
	}
}
