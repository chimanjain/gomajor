package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chimanjain/gomajor/checker"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

func TestRunner(t *testing.T) {
	t.Run("RunChecker", func(t *testing.T) {
		tests := []struct {
			name         string
			goModContent string
			checkAll     bool
			maxProbe     int
			jsonOutput   bool
			httpHandler  func(rw http.ResponseWriter, req *http.Request)
			wantErr      bool
		}{
			{
				name: "NoDirectDeps",
				goModContent: `module example.com/test

go 1.21

require github.com/google/uuid v1.6.0 // indirect
`,
				checkAll: false,
				maxProbe: 0,
			},
			{
				name:         "EmptyMod",
				goModContent: "module example.com/empty\n\ngo 1.21\n",
				checkAll:     false,
				maxProbe:     0,
			},
			{
				name: "WithUpdatesMock",
				goModContent: `module example.com/test

go 1.21

require github.com/foo/bar v1.0.0
`,
				checkAll: false,
				maxProbe: 2,
				httpHandler: func(rw http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
						_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
					} else {
						rw.WriteHeader(http.StatusNotFound)
					}
				},
			},
			{
				name: "AllDeps",
				goModContent: `module example.com/test

go 1.21

require (
	github.com/foo/bar v1.0.0
	github.com/foo/baz v1.0.0 // indirect
)
`,
				checkAll: true,
				maxProbe: 1,
				httpHandler: func(rw http.ResponseWriter, req *http.Request) {
					rw.WriteHeader(http.StatusNotFound)
				},
			},
			{
				name: "Json",
				goModContent: `module example.com/test
require github.com/foo/bar v1.0.0
`,
				checkAll:   false,
				maxProbe:   2,
				jsonOutput: true,
				httpHandler: func(rw http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
						_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
					} else {
						rw.WriteHeader(http.StatusNotFound)
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir := t.TempDir()
				p := writeModFile(t, dir, tt.goModContent)

				var server *httptest.Server
				if tt.httpHandler != nil {
					server = httptest.NewServer(http.HandlerFunc(tt.httpHandler))
				} else {
					server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
						rw.WriteHeader(http.StatusNotFound)
					}))
				}
				defer server.Close()

				cfg := &Config{
					ModFilePath: p,
					CheckAll:    tt.checkAll,
					MaxProbe:    tt.maxProbe,
					Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
				}

				err := runCheckerWithConfig(context.Background(), cfg, true, tt.jsonOutput, false)
				if (err != nil) != tt.wantErr {
					t.Fatalf("runCheckerWithConfig() returned error = %v, wantErr = %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("SingleCheckerFileOutput", func(t *testing.T) {
		dir := t.TempDir()
		p := writeModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			} else {
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		tests := []struct {
			ext string
		}{
			{".json"},
			{".yaml"},
		}

		for _, tt := range tests {
			t.Run(tt.ext, func(t *testing.T) {
				outPath := filepath.Join(dir, "report"+tt.ext)
				cfg := &Config{
					ModFilePath: p,
					MaxProbe:    2,
					OutputPath:  outPath,
					Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
				}

				if err := runCheckerWithConfig(context.Background(), cfg, true, false, true); err != nil {
					t.Fatalf("runCheckerWithConfig failed for %s: %v", tt.ext, err)
				}

				bytes, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatalf("failed to read %s output: %v", tt.ext, err)
				}

				var output YAMLOutput
				if tt.ext == ".json" {
					err = json.Unmarshal(bytes, &output)
				} else {
					err = yaml.Unmarshal(bytes, &output)
				}
				if err != nil {
					t.Fatalf("unmarshal error for %s: %v", tt.ext, err)
				}

				if len(output.Results) != 1 || output.Results[0].Source != p {
					t.Errorf("unexpected output structure for %s: %+v", tt.ext, output)
				}
			})
		}
	})

	t.Run("RunMultiChecker", func(t *testing.T) {
		tests := []struct {
			name           string
			wantJsonOutput bool
			setup          func(t *testing.T, dir string, serverURL string) (configPath string, cfg *Config)
			wantResultsLen int
			verify         func(t *testing.T, output YAMLOutput, dir string)
		}{
			{
				name: "ConfigYaml",
				setup: func(t *testing.T, dir string, serverURL string) (string, *Config) {
					localModPath := filepath.Join(dir, "local.mod")
					localContent := `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
					if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					configPath := filepath.Join(dir, "gomajor.yaml")
					outputPath := filepath.Join(dir, "gomajor-report.yaml")

					yamlCfg := YAMLConfig{
						Local:  []string{localModPath},
						Github: []string{serverURL + "/owner/repo/main/go.mod"},
						Output: outputPath,
					}
					yamlBytes, err := yaml.Marshal(yamlCfg)
					if err != nil {
						t.Fatalf("yaml.Marshal: %v", err)
					}
					if err := os.WriteFile(configPath, yamlBytes, 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					return configPath, &Config{
						MaxProbe:   2,
						ConfigPath: configPath,
					}
				},
				wantResultsLen: 2,
				verify: func(t *testing.T, output YAMLOutput, dir string) {
					localFound := false
					for _, res := range output.Results {
						if filepath.Base(res.Source) == "local.mod" {
							localFound = true
							if res.SourceType != "local" {
								t.Errorf("source %s type = %q, want 'local'", res.Source, res.SourceType)
							}
							if len(res.Dependencies) != 1 {
								t.Fatalf("expected 1 dependency for local, got %d", len(res.Dependencies))
							}
							dep := res.Dependencies[0]
							if dep.Module != "github.com/foo/bar" || dep.CurrentVersion != "v1.0.0" || dep.LatestMajorVersion != "v2.0.0" || !dep.HasUpdate {
								t.Errorf("unexpected dependency info for local: %+v", dep)
							}
						}
					}
					if !localFound {
						t.Error("local go.mod results not found in output")
					}
				},
			},
			{
				name: "GithubReposDirectly",
				setup: func(t *testing.T, dir string, serverURL string) (string, *Config) {
					outputPath := filepath.Join(dir, "gomajor-report.yaml")
					return "", &Config{
						MaxProbe:    2,
						GitHubRepos: []string{serverURL + "/owner/repo/main/go.mod"},
						OutputPath:  outputPath,
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output YAMLOutput, _ string) {
					res := output.Results[0]
					if res.SourceType != "github" {
						t.Errorf("source type = %q, want 'github'", res.SourceType)
					}
					if len(res.Dependencies) != 1 {
						t.Fatalf("expected 1 dependency, got %d", len(res.Dependencies))
					}
					dep := res.Dependencies[0]
					if dep.Module != "github.com/foo/baz" || dep.CurrentVersion != "v1.0.0" || dep.LatestMajorVersion != "v2.5.0" || !dep.HasUpdate {
						t.Errorf("unexpected dependency info: %+v", dep)
					}
				},
			},
			{
				name: "ConfigYaml_DisableOptions",
				setup: func(t *testing.T, dir string, serverURL string) (string, *Config) {
					localModPath := filepath.Join(dir, "local.mod")
					localContent := `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
					if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					configPath := filepath.Join(dir, "gomajor.yaml")
					outputPath := filepath.Join(dir, "gomajor-report.yaml")

					disabled := false
					yamlCfg := YAMLConfig{
						Local:  []string{localModPath},
						Output: outputPath,
						Minor:  &disabled,
						Major:  &disabled,
					}
					yamlBytes, err := yaml.Marshal(yamlCfg)
					if err != nil {
						t.Fatalf("yaml.Marshal: %v", err)
					}
					if err := os.WriteFile(configPath, yamlBytes, 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					return configPath, &Config{
						MaxProbe:   2,
						ConfigPath: configPath,
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output YAMLOutput, dir string) {
					res := output.Results[0]
					if len(res.Dependencies) != 1 {
						t.Fatalf("expected 1 dependency for local, got %d", len(res.Dependencies))
					}
					dep := res.Dependencies[0]
					if dep.HasUpdate || dep.HasMinorUpdate {
						t.Errorf("unexpected update: %+v (both major and minor should be disabled)", dep)
					}
				},
			},
			{
				name:           "ConfigJson",
				wantJsonOutput: true,
				setup: func(t *testing.T, dir string, serverURL string) (string, *Config) {
					localModPath := filepath.Join(dir, "local.mod")
					localContent := `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
					if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					configPath := filepath.Join(dir, "gomajor.yaml")
					outputPath := filepath.Join(dir, "gomajor-report.json")

					yamlCfg := YAMLConfig{
						Local:  []string{localModPath},
						Github: []string{serverURL + "/owner/repo/main/go.mod"},
						Output: outputPath,
					}
					yamlBytes, err := yaml.Marshal(yamlCfg)
					if err != nil {
						t.Fatalf("yaml.Marshal: %v", err)
					}
					if err := os.WriteFile(configPath, yamlBytes, 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					return configPath, &Config{
						MaxProbe:   2,
						ConfigPath: configPath,
					}
				},
				wantResultsLen: 2,
				verify: func(t *testing.T, output YAMLOutput, dir string) {
					localFound := false
					for _, res := range output.Results {
						if filepath.Base(res.Source) == "local.mod" {
							localFound = true
							if res.SourceType != "local" {
								t.Errorf("source %s type = %q, want 'local'", res.Source, res.SourceType)
							}
							if len(res.Dependencies) != 1 {
								t.Fatalf("expected 1 dependency for local, got %d", len(res.Dependencies))
							}
							dep := res.Dependencies[0]
							if dep.Module != "github.com/foo/bar" || dep.CurrentVersion != "v1.0.0" || dep.LatestMajorVersion != "v2.0.0" || !dep.HasUpdate {
								t.Errorf("unexpected dependency info for local: %+v", dep)
							}
						}
					}
					if !localFound {
						t.Error("local go.mod results not found in JSON output")
					}
				},
			},
			{
				name: "DeduplicateSources",
				setup: func(t *testing.T, dir string, serverURL string) (string, *Config) {
					localModPath := filepath.Join(dir, "local.mod")
					localContent := `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
					if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					configPath := filepath.Join(dir, "gomajor.yaml")
					outputPath := filepath.Join(dir, "gomajor-report.yaml")

					yamlCfg := YAMLConfig{
						Local:  []string{localModPath, localModPath},
						Github: []string{serverURL + "/owner/repo/main/go.mod", serverURL + "/owner/repo/main/go.mod"},
						Output: outputPath,
					}
					yamlBytes, err := yaml.Marshal(yamlCfg)
					if err != nil {
						t.Fatalf("yaml.Marshal: %v", err)
					}
					if err := os.WriteFile(configPath, yamlBytes, 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					return configPath, &Config{
						MaxProbe:   2,
						ConfigPath: configPath,
					}
				},
				wantResultsLen: 2,
				verify: func(t *testing.T, output YAMLOutput, dir string) {
					localCount := 0
					githubCount := 0
					for _, res := range output.Results {
						switch res.SourceType {
						case "local":
							localCount++
						case "github":
							githubCount++
						}
					}
					if localCount != 1 {
						t.Errorf("expected exactly 1 local result due to deduplication, got %d", localCount)
					}
					if githubCount != 1 {
						t.Errorf("expected exactly 1 github result due to deduplication, got %d", githubCount)
					}
				},
			},
			{
				name: "SpaceAndCommaSeparatedGitHubRepos",
				setup: func(t *testing.T, dir string, serverURL string) (string, *Config) {
					outputPath := filepath.Join(dir, "gomajor-report.yaml")
					reposStr := fmt.Sprintf("%s/owner/repo/main/go.mod, %s/owner/repo/main/go.mod\t%s/owner/repo/main/go.mod", serverURL, serverURL, serverURL)
					return "", &Config{
						MaxProbe:    2,
						GitHubRepos: []string{reposStr},
						OutputPath:  outputPath,
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output YAMLOutput, _ string) {
					if len(output.Results) != 1 {
						t.Fatalf("expected 1 result, got %d", len(output.Results))
					}
					res := output.Results[0]
					if res.SourceType != "github" {
						t.Errorf("source type = %q, want 'github'", res.SourceType)
					}
					if len(res.Dependencies) != 1 {
						t.Fatalf("expected 1 dependency, got %d", len(res.Dependencies))
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir := t.TempDir()

				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/owner/repo/main/go.mod" {
						_, _ = rw.Write([]byte(`module example.com/remote
go 1.21
require github.com/foo/baz v1.0.0
`))
						return
					}

					if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
						_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
						return
					}
					if req.URL.Path == "/github.com/foo/baz/v2/@latest" {
						_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.5.0"})
						return
					}

					rw.WriteHeader(http.StatusNotFound)
				}))
				defer server.Close()

				configPath, testConfig := tt.setup(t, dir, server.URL)
				testConfig.Client = &checker.Client{
					HTTPClient: server.Client(),
					ProxyBase:  server.URL,
				}

				outputPath := testConfig.OutputPath
				if configPath != "" {
					if tt.wantJsonOutput {
						outputPath = filepath.Join(dir, "gomajor-report.json")
					} else {
						outputPath = filepath.Join(dir, "gomajor-report.yaml")
					}
				}

				err := runCheckerWithConfig(context.Background(), testConfig, false, false, true)
				if err != nil {
					t.Fatalf("runCheckerWithConfig failed: %v", err)
				}

				if _, err := os.Stat(outputPath); os.IsNotExist(err) {
					t.Fatalf("expected output file %s does not exist", outputPath)
				}

				outContent, err := os.ReadFile(outputPath)
				if err != nil {
					t.Fatalf("failed to read output file: %v", err)
				}

				var output YAMLOutput
				if tt.wantJsonOutput {
					if err := json.Unmarshal(outContent, &output); err != nil {
						t.Fatalf("failed to unmarshal output JSON: %v", err)
					}
				} else {
					if err := yaml.Unmarshal(outContent, &output); err != nil {
						t.Fatalf("failed to unmarshal output YAML: %v", err)
					}
				}

				if len(output.Results) != tt.wantResultsLen {
					t.Errorf("len(output.Results) = %d, want %d", len(output.Results), tt.wantResultsLen)
				}

				tt.verify(t, output, dir)
			})
		}
	})

	t.Run("PrintMultiJsonResults", func(t *testing.T) {
		results := []SourceResult{
			{
				Source:     "test-source",
				SourceType: "local",
				Dependencies: []DependencyInfo{
					{
						Module:             "github.com/foo/bar",
						CurrentVersion:     "v1.0.0",
						LatestMajorVersion: "v2.0.0",
						LatestMajorPath:    "github.com/foo/bar/v2",
						HasUpdate:          true,
					},
				},
			},
		}

		var buf bytes.Buffer
		err := printMultiJsonResults(&buf, results)
		if err != nil {
			t.Fatalf("printMultiJsonResults failed: %v", err)
		}

		var output YAMLOutput
		if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
			t.Fatalf("failed to unmarshal stdout JSON output: %v", err)
		}

		if len(output.Results) != 1 || output.Results[0].Source != "test-source" {
			t.Errorf("unexpected output struct: %+v", output)
		}
	})

	t.Run("CheckDependencies_Cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.0.0"})
		}))
		defer server.Close()

		client := &checker.Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		}

		cfg := &Config{
			Client:   client,
			MaxProbe: 0,
			Minor:    true,
		}
		cfg.Client.DisableMinor = false
		cfg.Client.DisableMajor = true

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancel the context

		var reqs []*modfile.Require
		for i := 1; i <= 5; i++ {
			reqs = append(reqs, &modfile.Require{
				Mod: module.Version{
					Path:    fmt.Sprintf("github.com/foo/bar%d", i),
					Version: "v1.0.0",
				},
			})
		}

		results := checkDependencies(ctx, cfg, reqs)
		if len(results) != 0 {
			t.Errorf("Expected 0 results for cancelled context, got %d", len(results))
		}
	})

	t.Run("SanitizeURL", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{
				input:    "https://token:x-oauth-basic@github.com/owner/repo/go.mod",
				expected: "https://redacted@github.com/owner/repo/go.mod",
			},
			{
				input:    "https://user:password@git.internal.corp/project/go.mod",
				expected: "https://redacted@git.internal.corp/project/go.mod",
			},
			{
				input:    "https://github.com/owner/repo",
				expected: "https://github.com/owner/repo",
			},
			{
				input:    "/run/media/chiman/Data/github/gomajor/go.mod",
				expected: "/run/media/chiman/Data/github/gomajor/go.mod",
			},
		}

		for _, tt := range tests {
			got := sanitizeURL(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		}
	})
}
