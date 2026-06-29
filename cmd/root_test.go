package cmd

import (
	"os"
	"path/filepath"
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
				goModPath := filepath.Join(dir, "go.mod")
				if err := os.WriteFile(goModPath, []byte("module example.com/test\n\ngo 1.21\n"), 0o644); err != nil {
					t.Fatalf("failed to create temp go.mod: %v", err)
				}
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

func TestRootCmd_Flags(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		verifyDefaults bool
		wantMaxProbe   int
		wantCheckAll   bool
		wantNoColor    bool
		wantRepos      []string
		wantErr        bool
	}{
		{
			name:           "Defaults",
			verifyDefaults: true,
			wantMaxProbe:   5,
			wantCheckAll:   false,
			wantNoColor:    false,
		},
		{
			name:      "GithubFlag_SingleRepo",
			args:      []string{"-g", "owner/repo"},
			wantRepos: []string{"owner/repo"},
		},
		{
			name:      "GithubFlag_CommaSeparated",
			args:      []string{"-g", "owner/repo1,github.com/owner/repo2"},
			wantRepos: []string{"owner/repo1", "github.com/owner/repo2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear/reset state before run
			config.GitHubRepos = nil
			if err := rootCmd.Flags().Set("github", ""); err != nil {
				t.Fatalf("failed to reset flag: %v", err)
			}

			if tt.verifyDefaults {
				if config.MaxProbe != tt.wantMaxProbe {
					t.Errorf("default MaxProbe = %v, want %v", config.MaxProbe, tt.wantMaxProbe)
				}
				if config.CheckAll != tt.wantCheckAll {
					t.Errorf("default CheckAll = %v, want %v", config.CheckAll, tt.wantCheckAll)
				}
				if config.NoColor != tt.wantNoColor {
					t.Errorf("default NoColor = %v, want %v", config.NoColor, tt.wantNoColor)
				}
				return
			}

			err := rootCmd.ParseFlags(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFlags() returned error = %v, wantErr = %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if len(config.GitHubRepos) != len(tt.wantRepos) {
					t.Fatalf("len(config.GitHubRepos) = %d, want %d", len(config.GitHubRepos), len(tt.wantRepos))
				}
				for i, v := range config.GitHubRepos {
					if v != tt.wantRepos[i] {
						t.Errorf("config.GitHubRepos[%d] = %q, want %q", i, v, tt.wantRepos[i])
					}
				}
			}
		})
	}

	// Clean up config.GitHubRepos after all subtests complete
	config.GitHubRepos = nil
}
