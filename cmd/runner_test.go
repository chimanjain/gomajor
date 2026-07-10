package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chimanjain/gomajor/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/chimanjain/gomajor/pkg/format"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const (
	versionKey      = "Version"
	localModContent = `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
	localSource        = "local"
	githubSource       = "github"
	fooBarModule       = "github.com/foo/bar"
	githubOwnerRepoURL = "https://github.com/owner/repo"
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
						_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
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
				httpHandler: func(rw http.ResponseWriter, _ *http.Request) {
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
						_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
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
					server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
						rw.WriteHeader(http.StatusNotFound)
					}))
				}
				defer server.Close()

				cfg := &config.Config{
					ModFilePath: p,
					CheckAll:    tt.checkAll,
					MaxProbe:    tt.maxProbe,
					Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
				}

				err := runCheckerWithConfig(context.Background(), cfg, true, tt.jsonOutput, false, false, false)
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
				_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
			} else {
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		tests := []struct {
			ext string
		}{
			{jsonExt},
			{".yaml"},
		}

		for _, tt := range tests {
			t.Run(tt.ext, func(t *testing.T) {
				outPath := filepath.Join(dir, "report"+tt.ext)
				cfg := &config.Config{
					ModFilePath: p,
					MaxProbe:    2,
					OutputPath:  outPath,
					Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
				}

				if err := runCheckerWithConfig(context.Background(), cfg, true, false, true, false, false); err != nil {
					t.Fatalf("runCheckerWithConfig failed for %s: %v", tt.ext, err)
				}

				bytes, err := os.ReadFile(outPath)
				if err != nil {
					t.Fatalf("failed to read %s output: %v", tt.ext, err)
				}

				var output format.YAMLOutput
				if tt.ext == jsonExt {
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
		setupConfig := func(ext string) func(t *testing.T, dir string, serverURL string) (string, *config.Config) {
			return func(t *testing.T, dir string, serverURL string) (string, *config.Config) {
				localModPath := filepath.Join(dir, "local.mod")
				localContent := localModContent
				if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
					t.Fatalf("os.WriteFile: %v", err)
				}

				configPath := filepath.Join(dir, "gomajor.yaml")
				outputPath := filepath.Join(dir, "gomajor-report."+ext)

				yamlCfg := config.YAMLConfig{
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

				return configPath, &config.Config{
					MaxProbe:   2,
					ConfigPath: configPath,
				}
			}
		}

		verifyConfig := func(t *testing.T, output format.YAMLOutput, _ string) {
			localFound := false
			for _, res := range output.Results {
				if filepath.Base(res.Source) == "local.mod" {
					localFound = true
					if res.SourceType != localSource {
						t.Errorf("source %s type = %q, want 'local'", res.Source, res.SourceType)
					}
					if len(res.Dependencies) != 1 {
						t.Fatalf("expected 1 dependency for local, got %d", len(res.Dependencies))
					}
					dep := res.Dependencies[0]
					if dep.Module != fooBarModule || dep.CurrentVersion != "v1.0.0" || dep.LatestMajorVersion != "v2.0.0" || !dep.HasUpdate {
						t.Errorf("unexpected dependency info for local: %+v", dep)
					}
				}
			}
			if !localFound {
				t.Error("local go.mod results not found in output")
			}
		}

		tests := []struct {
			name           string
			wantJSONOutput bool
			setup          func(t *testing.T, dir string, serverURL string) (configPath string, cfg *config.Config)
			wantResultsLen int
			verify         func(t *testing.T, output format.YAMLOutput, dir string)
		}{
			{
				name:           "ConfigYaml",
				setup:          setupConfig("yaml"),
				wantResultsLen: 2,
				verify:         verifyConfig,
			},
			{
				name: "GithubReposDirectly",
				setup: func(_ *testing.T, dir string, serverURL string) (string, *config.Config) {
					outputPath := filepath.Join(dir, "gomajor-report.yaml")
					return "", &config.Config{
						MaxProbe:    2,
						GitHubRepos: []string{serverURL + "/owner/repo/main/go.mod"},
						OutputPath:  outputPath,
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output format.YAMLOutput, _ string) {
					res := output.Results[0]
					if res.SourceType != githubSource {
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
				setup: func(t *testing.T, dir string, _ string) (string, *config.Config) {
					localModPath := filepath.Join(dir, "local.mod")
					localContent := localModContent
					if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					configPath := filepath.Join(dir, "gomajor.yaml")
					outputPath := filepath.Join(dir, "gomajor-report.yaml")

					disabled := false
					yamlCfg := config.YAMLConfig{
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

					return configPath, &config.Config{
						MaxProbe:   2,
						ConfigPath: configPath,
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output format.YAMLOutput, _ string) {
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
				wantJSONOutput: true,
				setup:          setupConfig("json"),
				wantResultsLen: 2,
				verify:         verifyConfig,
			},
			{
				name: "DeduplicateSources",
				setup: func(t *testing.T, dir string, serverURL string) (string, *config.Config) {
					localModPath := filepath.Join(dir, "local.mod")
					localContent := localModContent
					if err := os.WriteFile(localModPath, []byte(localContent), 0o644); err != nil {
						t.Fatalf("os.WriteFile: %v", err)
					}

					configPath := filepath.Join(dir, "gomajor.yaml")
					outputPath := filepath.Join(dir, "gomajor-report.yaml")

					yamlCfg := config.YAMLConfig{
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

					return configPath, &config.Config{
						MaxProbe:   2,
						ConfigPath: configPath,
					}
				},
				wantResultsLen: 2,
				verify: func(t *testing.T, output format.YAMLOutput, _ string) {
					localCount := 0
					githubCount := 0
					for _, res := range output.Results {
						switch res.SourceType {
						case localSource:
							localCount++
						case githubSource:
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
				setup: func(_ *testing.T, dir string, serverURL string) (string, *config.Config) {
					outputPath := filepath.Join(dir, "gomajor-report.yaml")
					reposStr := fmt.Sprintf("%s/owner/repo/main/go.mod, %s/owner/repo/main/go.mod\t%s/owner/repo/main/go.mod", serverURL, serverURL, serverURL)
					return "", &config.Config{
						MaxProbe:    2,
						GitHubRepos: []string{reposStr},
						OutputPath:  outputPath,
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output format.YAMLOutput, _ string) {
					if len(output.Results) != 1 {
						t.Fatalf("expected 1 result, got %d", len(output.Results))
					}
					res := output.Results[0]
					if res.SourceType != githubSource {
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
						_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
						return
					}
					if req.URL.Path == "/github.com/foo/baz/v2/@latest" {
						_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.5.0"})
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
					if tt.wantJSONOutput {
						outputPath = filepath.Join(dir, "gomajor-report.json")
					} else {
						outputPath = filepath.Join(dir, "gomajor-report.yaml")
					}
				}

				err := runCheckerWithConfig(context.Background(), testConfig, false, false, true, false, false)
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

				var output format.YAMLOutput
				if tt.wantJSONOutput {
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

	t.Run("CheckDependencies_Cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v1.0.0"})
		}))
		defer server.Close()

		client := &checker.Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		}

		cfg := &config.Config{
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

		cfg.Client.HTTPClient = server.Client()
		eng := engine.New(cfg)
		results, _ := eng.CheckDependencies(ctx, reqs)
		if len(results) != 0 {
			t.Errorf("Expected 0 results for cancelled context, got %d", len(results))
		}
	})

	t.Run("ImplicitConfigParseError", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "gomajor.yaml")
		if err := os.WriteFile(configPath, []byte("malformed: yaml: :"), 0o644); err != nil {
			t.Fatalf("os.WriteFile: %v", err)
		}

		origCwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("os.Chdir: %v", err)
		}
		defer func() {
			_ = os.Chdir(origCwd)
		}()

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		cfg := &config.Config{
			ConfigPath: "",
			Client:     &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
		}

		err = runCheckerWithConfig(context.Background(), cfg, false, false, false, false, false)
		if err == nil {
			t.Fatal("expected error parsing malformed implicit config, got nil")
		}
	})

	t.Run("CliPrecedence", func(t *testing.T) {
		dir := t.TempDir()
		p := writeModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		configPath := filepath.Join(dir, "gomajor.yaml")
		configContent := fmt.Sprintf(`
local:
  - %s
minor: false
major: false
`, p)
		if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			} else {
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		// case 1: minorExplicit and majorExplicit are true (CLI passed --minor=true --major=true)
		// We expect the CLI values (true, true) to override the config file values (false, false)
		cfg := &config.Config{
			ModFilePath: p,
			ConfigPath:  configPath,
			Minor:       true,
			Major:       true,
			MaxProbe:    2,
			Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
		}
		err := runCheckerWithConfig(context.Background(), cfg, true, true, false, true, true)
		if err != nil {
			t.Fatalf("runCheckerWithConfig failed: %v", err)
		}
		if !cfg.Minor {
			t.Error("expected cfg.Minor to remain true (CLI precedence), but got false")
		}
		if !cfg.Major {
			t.Error("expected cfg.Major to remain true (CLI precedence), but got false")
		}

		// case 2: minorExplicit and majorExplicit are false (CLI defaults/not explicitly passed)
		// We expect config values (false, false) to override defaults.
		cfg2 := &config.Config{
			ModFilePath: p,
			ConfigPath:  configPath,
			Minor:       true,
			Major:       true,
			MaxProbe:    2,
			Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
		}
		err = runCheckerWithConfig(context.Background(), cfg2, true, true, false, false, false)
		if err != nil {
			t.Fatalf("runCheckerWithConfig failed: %v", err)
		}
		if cfg2.Minor {
			t.Error("expected cfg2.Minor to become false (config file took precedence), but got true")
		}
		if cfg2.Major {
			t.Error("expected cfg2.Major to become false (config file took precedence), but got true")
		}
	})
}
