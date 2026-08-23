package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/pkg/config"
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
				writeModFile(t, dir, "module example.com/test\n\ngo 1.21\n")
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
			name:      "Flags_MaxProbe_Capped",
			args:      []string{"-m", "999"},
			parseOnly: true,
			checkFn: func(t *testing.T, cmd *cobra.Command, _ string) {
				cfg, _, err := parseConfig(cmd)
				if err != nil {
					t.Fatalf("parseConfig: %v", err)
				}
				if cfg.MaxProbe != 50 {
					t.Errorf("MaxProbe = %v, want 50 (capped)", cfg.MaxProbe)
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
				want := Version + "\n"
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			},
		},
		{
			name: "Help",
			args: []string{"--help"},
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
			dir := t.TempDir()
			writeModFile(t, dir, "module example.com/test\ngo 1.21\n")
			t.Chdir(dir)

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

		cfg, yamlCfg, err := parseConfig(cmd)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}

		if cfg.ConfigPath != "" {
			t.Errorf("expected ConfigPath to be empty when -f is explicitly passed, got %q", cfg.ConfigPath)
		}
		if cfg.ModFilePath != "custom/go.mod" {
			t.Errorf("expected ModFilePath to be custom/go.mod, got %q", cfg.ModFilePath)
		}
		if !isSingleMode(cfg, yamlCfg) {
			t.Errorf("expected isSingleMode to be true when -f is passed without explicit -c")
		}
	})

	t.Run("ExplicitNonExistentConfig_Error", func(t *testing.T) {
		cmd := NewRootCmd()
		args := []string{"-c", "does-not-exist.yaml"}
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}

		_, _, err := parseConfig(cmd)
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

		_, _, err := parseConfig(cmd)
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
		cfgJSON, _, err := parseConfig(cmdJSON)
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
		cfgYAML, _, err := parseConfig(cmdYAML)
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
		name    string
		cfg     *config.Config
		yamlCfg config.YAMLConfig
		want    bool
	}{
		{
			name:    "SingleLocal",
			cfg:     &config.Config{},
			yamlCfg: config.YAMLConfig{},
			want:    true,
		},
		{
			name:    "HasConfigPath",
			cfg:     &config.Config{ConfigPath: "gomajor.yaml"},
			yamlCfg: config.YAMLConfig{},
			want:    false,
		},
		{
			name:    "HasCliGithub",
			cfg:     &config.Config{GitHubRepos: []string{"owner/repo"}},
			yamlCfg: config.YAMLConfig{},
			want:    false,
		},
		{
			name:    "HasYamlLocal",
			cfg:     &config.Config{},
			yamlCfg: config.YAMLConfig{Local: []string{"sub/go.mod"}},
			want:    false,
		},
		{
			name:    "HasYamlGithub",
			cfg:     &config.Config{},
			yamlCfg: config.YAMLConfig{Github: []string{"owner/repo"}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSingleMode(tt.cfg, tt.yamlCfg)
			if got != tt.want {
				t.Errorf("isSingleMode() = %t, want %t", got, tt.want)
			}
		})
	}
}

func writeModFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writeModFile: %v", err)
	}
	return p
}
