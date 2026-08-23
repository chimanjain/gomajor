package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/chimanjain/gomajor/pkg/source"
	"go.yaml.in/yaml/v3"
)

const (
	versionKey      = "Version"
	localModContent = `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
	fooBarModule = "github.com/foo/bar"
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
					JSONOutput:  tt.jsonOutput,
					Minor:       true,
				}
				rt := &Runtime{
					Config: cfg,
					Client: &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
				}

				err := runChecker(context.Background(), rt, true)
				if (err != nil) != tt.wantErr {
					t.Fatalf("runChecker() returned error = %v, wantErr = %v", err, tt.wantErr)
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
					Minor:       true,
				}
				rt := &Runtime{
					Config: cfg,
					Client: &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
				}

				if err := runChecker(context.Background(), rt, true); err != nil {
					t.Fatalf("runChecker failed for %s: %v", tt.ext, err)
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
		setupConfig := func(ext string) func(t *testing.T, dir string, serverURL string) (string, *config.Config, config.YAMLConfig) {
			return func(t *testing.T, dir string, serverURL string) (string, *config.Config, config.YAMLConfig) {
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
					Minor:      true,
				}, yamlCfg
			}
		}

		verifyConfig := func(t *testing.T, output format.YAMLOutput, _ string) {
			localFound := false
			for _, res := range output.Results {
				if filepath.Base(res.Source) == "local.mod" {
					localFound = true
					if res.SourceType != source.Local {
						t.Errorf("source %s type = %q, want %q", res.Source, res.SourceType, source.Local)
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
			setup          func(t *testing.T, dir string, serverURL string) (configPath string, cfg *config.Config, yamlCfg config.YAMLConfig)
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
				setup: func(_ *testing.T, dir string, serverURL string) (string, *config.Config, config.YAMLConfig) {
					outputPath := filepath.Join(dir, "gomajor-report.yaml")
					return "", &config.Config{
						MaxProbe:    2,
						GitHubRepos: []string{serverURL + "/owner/repo/main/go.mod"},
						OutputPath:  outputPath,
						Minor:       true,
					}, config.YAMLConfig{
						Github: []string{serverURL + "/owner/repo/main/go.mod"},
					}
				},
				wantResultsLen: 1,
				verify: func(t *testing.T, output format.YAMLOutput, _ string) {
					res := output.Results[0]
					if res.SourceType != source.GitHub {
						t.Errorf("source type = %q, want %q", res.SourceType, source.GitHub)
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
				name:           "ConfigJson",
				wantJSONOutput: true,
				setup:          setupConfig("json"),
				wantResultsLen: 2,
				verify:         verifyConfig,
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

				configPath, testConfig, testYamlCfg := tt.setup(t, dir, server.URL)
				client := &checker.Client{
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
					testConfig.OutputPath = outputPath
					testYamlCfg.Output = outputPath
				}

				rt := &Runtime{
					Config:           testConfig,
					YAMLConfig:       testYamlCfg,
					Client:           client,
					GitHubHTTPClient: server.Client(),
				}

				singleMode := isSingleMode(testConfig, testYamlCfg)
				err := runChecker(context.Background(), rt, singleMode)
				if err != nil {
					t.Fatalf("runChecker failed: %v", err)
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

	t.Run("DefaultsAndNilHandling", func(t *testing.T) {
		// When rt is initialized with nil fields, runChecker should apply defaults safely
		rt := &Runtime{}
		err := runChecker(context.Background(), rt, true)
		if err == nil {
			t.Error("expected error resolving non-existent go.mod, got nil")
		}
	})

	t.Run("NoColorRestoration", func(t *testing.T) {
		dir := t.TempDir()
		p := writeModFile(t, dir, "module example.com/test\ngo 1.21\n")

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		rt := &Runtime{
			Config: &config.Config{
				ModFilePath: p,
				NoColor:     true,
			},
			Client: &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
		}

		err := runChecker(context.Background(), rt, true)
		if err != nil {
			t.Fatalf("runChecker failed: %v", err)
		}
	})

	t.Run("StdoutJSONOutput", func(t *testing.T) {
		dir := t.TempDir()
		p := writeModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
				_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
				return
			}
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		var stdout bytes.Buffer
		rt := &Runtime{
			Config: &config.Config{
				ModFilePath: p,
				JSONOutput:  true,
				OutputPath:  "",
				MaxProbe:    2,
			},
			Client: &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
			Out:    &stdout,
		}

		err := runChecker(context.Background(), rt, true)
		if err != nil {
			t.Fatalf("runChecker failed: %v", err)
		}

		var out format.YAMLOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("failed to parse stdout JSON: %v", err)
		}
		if len(out.Results) != 1 {
			t.Errorf("len(out.Results) = %d, want 1", len(out.Results))
		}
	})

	t.Run("ProgressCallbackInvoked", func(t *testing.T) {
		dir := t.TempDir()
		p := writeModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
				_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
				return
			}
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		progressCalls := 0
		rt := &Runtime{
			Config: &config.Config{
				ModFilePath: p,
				MaxProbe:    2,
			},
			Client: &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
			OnProgress: func(completed, total int) {
				progressCalls++
			},
		}

		err := runChecker(context.Background(), rt, true)
		if err != nil {
			t.Fatalf("runChecker failed: %v", err)
		}

		if progressCalls == 0 {
			t.Error("expected OnProgress callback to be called at least once")
		}
	})
}
