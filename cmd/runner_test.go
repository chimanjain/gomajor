package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/checker"
	"go.yaml.in/yaml/v3"
)

func TestRunCheckerTable(t *testing.T) {
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

			var client *checker.Client
			if tt.httpHandler != nil {
				server := httptest.NewServer(http.HandlerFunc(tt.httpHandler))
				defer server.Close()
				client = &checker.Client{
					HTTPClient: server.Client(),
					ProxyBase:  server.URL,
				}
			} else {
				client = checker.DefaultClient()
			}

			testConfig := &Config{
				ModFilePath: p,
				CheckAll:    tt.checkAll,
				MaxProbe:    tt.maxProbe,
				JsonOutput:  tt.jsonOutput,
				Client:      client,
			}

			err := runCheckerWithConfig(testConfig, true, false, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("runCheckerWithConfig returned error: %v, wantErr: %v", err, tt.wantErr)
			}
		})
	}
}

func TestSingleCheckerFileOutput(t *testing.T) {
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

	for _, ext := range []string{".json", ".yaml"} {
		outPath := filepath.Join(dir, "report"+ext)
		cfg := &Config{
			ModFilePath: p,
			MaxProbe:    2,
			OutputPath:  outPath,
			Client:      &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
		}

		if err := runCheckerWithConfig(cfg, true, false, true); err != nil {
			t.Fatalf("runCheckerWithConfig failed for %s: %v", ext, err)
		}

		bytes, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("failed to read %s output: %v", ext, err)
		}

		var output YAMLOutput
		if ext == ".json" {
			err = json.Unmarshal(bytes, &output)
		} else {
			err = yaml.Unmarshal(bytes, &output)
		}
		if err != nil {
			t.Fatalf("unmarshal error for %s: %v", ext, err)
		}

		if len(output.Results) != 1 || output.Results[0].Source != p {
			t.Errorf("unexpected output structure for %s: %+v", ext, output)
		}
	}
}

func TestRunMultiCheckerTable(t *testing.T) {
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
				if err := os.WriteFile(localModPath, []byte(localContent), 0644); err != nil {
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
				if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
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
					GithubRepos: []string{serverURL + "/owner/repo/main/go.mod"},
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
				if err := os.WriteFile(localModPath, []byte(localContent), 0644); err != nil {
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
				if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
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
				if err := os.WriteFile(localModPath, []byte(localContent), 0644); err != nil {
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
				if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
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
				if err := os.WriteFile(localModPath, []byte(localContent), 0644); err != nil {
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
				if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
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

			err := runCheckerWithConfig(testConfig, false, false, true)
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
}

func TestPrintMultiJsonResults(t *testing.T) {
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

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := printMultiJsonResults(results)
	if err != nil {
		t.Fatalf("printMultiJsonResults failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = old

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)

	var output YAMLOutput
	if err := json.Unmarshal([]byte(buf.String()), &output); err != nil {
		t.Fatalf("failed to unmarshal stdout JSON output: %v", err)
	}

	if len(output.Results) != 1 || output.Results[0].Source != "test-source" {
		t.Errorf("unexpected output struct: %+v", output)
	}
}
