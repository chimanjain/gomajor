package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/internal/testutil"
	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/chimanjain/gomajor/pkg/source"
	"go.yaml.in/yaml/v4"
)

func newTestRuntime(cfg *config.Config, server *httptest.Server) *Runtime {
	return &Runtime{
		Config: cfg,
		Client: &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
	}
}

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
			versions     map[string]string
			wantErr      bool
		}{
			{
				name:         "NoDirectDeps",
				goModContent: "module example.com/test\n\ngo 1.21\n\nrequire github.com/google/uuid v1.6.0 // indirect\n",
			},
			{
				name:         "EmptyMod",
				goModContent: "module example.com/empty\n\ngo 1.21\n",
			},
			{
				name:         "WithUpdatesMock",
				goModContent: "module example.com/test\n\ngo 1.21\n\nrequire github.com/foo/bar v1.0.0\n",
				maxProbe:     2,
				versions:     map[string]string{"/github.com/foo/bar/v2/@latest": "v2.0.0"},
			},
			{
				name:         "AllDeps",
				goModContent: "module example.com/test\n\ngo 1.21\n\nrequire (\n\tgithub.com/foo/bar v1.0.0\n\tgithub.com/foo/baz v1.0.0 // indirect\n)\n",
				checkAll:     true,
				maxProbe:     1,
			},
			{
				name:         "Json",
				goModContent: "module example.com/test\nrequire github.com/foo/bar v1.0.0\n",
				maxProbe:     2,
				jsonOutput:   true,
				versions:     map[string]string{"/github.com/foo/bar/v2/@latest": "v2.0.0"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir := t.TempDir()
				p := testutil.WriteModFile(t, dir, tt.goModContent)

				server := testutil.NewMockProxy(tt.versions)
				defer server.Close()

				rt := newTestRuntime(&config.Config{
					ModFilePath: p,
					CheckAll:    tt.checkAll,
					MaxProbe:    tt.maxProbe,
					JSONOutput:  tt.jsonOutput,
					Minor:       true,
				}, server)

				err := runChecker(context.Background(), rt, true)
				if (err != nil) != tt.wantErr {
					t.Fatalf("runChecker() returned error = %v, wantErr = %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("SingleCheckerFileOutput", func(t *testing.T) {
		dir := t.TempDir()
		p := testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		server := testutil.NewMockProxy(map[string]string{"/github.com/foo/bar/v2/@latest": "v2.0.0"})
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
				rt := newTestRuntime(&config.Config{
					ModFilePath: p,
					MaxProbe:    2,
					OutputPath:  outPath,
					Minor:       true,
				}, server)

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
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/owner/repo/main/go.mod":
				_, _ = rw.Write([]byte("module example.com/remote\ngo 1.21\nrequire github.com/foo/baz v1.0.0\n"))
			case "/github.com/foo/bar/v2/@latest":
				_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.0.0"})
			case "/github.com/foo/baz/v2/@latest":
				_ = json.NewEncoder(rw).Encode(map[string]string{versionKey: "v2.5.0"})
			default:
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		mockTransport := testutil.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "raw.githubusercontent.com") || strings.Contains(req.URL.Host, "github.com") {
				target, _ := url.Parse(server.URL + req.URL.Path)
				cloned := req.Clone(req.Context())
				cloned.URL = target
				cloned.Host = target.Host
				return server.Client().Transport.RoundTrip(cloned)
			}
			return server.Client().Transport.RoundTrip(req)
		})

		tests := []struct {
			name       string
			isDirectGH bool
			isJSON     bool
		}{
			{"ConfigYaml", false, false},
			{"GithubReposDirectly", true, false},
			{"ConfigJson", false, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir := t.TempDir()
				localMod := testutil.WriteConfigFile(t, dir, "local.mod", localModContent)
				outExt := ".yaml"
				if tt.isJSON {
					outExt = jsonExt
				}
				outputPath := filepath.Join(dir, "gomajor-report"+outExt)

				var testConfig *config.Config

				if tt.isDirectGH {
					testConfig = &config.Config{
						MaxProbe:    2,
						GitHubRepos: []string{"https://raw.githubusercontent.com/owner/repo/main/go.mod"},
						OutputPath:  outputPath,
						Minor:       true,
					}
				} else {
					configPath := filepath.Join(dir, "gomajor.yaml")
					testYamlCfg := config.YAMLConfig{
						Local:  []string{localMod},
						Github: []string{"https://raw.githubusercontent.com/owner/repo/main/go.mod"},
						Output: outputPath,
					}
					yamlBytes, err := yaml.Marshal(testYamlCfg)
					if err != nil {
						t.Fatalf("yaml.Marshal: %v", err)
					}
					testutil.WriteConfigFile(t, dir, "gomajor.yaml", string(yamlBytes))

					testConfig = &config.Config{
						MaxProbe:    2,
						ConfigPath:  configPath,
						OutputPath:  outputPath,
						LocalMods:   []string{localMod},
						GitHubRepos: []string{"https://raw.githubusercontent.com/owner/repo/main/go.mod"},
						Minor:       true,
					}
				}

				rt := &Runtime{
					Config:           testConfig,
					Client:           &checker.Client{HTTPClient: server.Client(), ProxyBase: server.URL},
					GitHubHTTPClient: &http.Client{Transport: mockTransport},
				}

				singleMode := testConfig.IsSingleMode()
				if err := runChecker(context.Background(), rt, singleMode); err != nil {
					t.Fatalf("runChecker failed: %v", err)
				}

				outContent, err := os.ReadFile(outputPath)
				if err != nil {
					t.Fatalf("failed to read output file: %v", err)
				}

				var output format.YAMLOutput
				if tt.isJSON {
					err = json.Unmarshal(outContent, &output)
				} else {
					err = yaml.Unmarshal(outContent, &output)
				}
				if err != nil {
					t.Fatalf("failed to unmarshal output: %v", err)
				}

				if tt.isDirectGH {
					if len(output.Results) != 1 || output.Results[0].SourceType != source.GitHub {
						t.Errorf("unexpected direct github results: %+v", output.Results)
					}
				} else {
					if len(output.Results) != 2 {
						t.Errorf("expected 2 results, got %d", len(output.Results))
					}
				}
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
		p := testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\n")

		server := testutil.NewMockProxy(nil)
		defer server.Close()

		rt := newTestRuntime(&config.Config{
			ModFilePath: p,
			NoColor:     true,
		}, server)

		err := runChecker(context.Background(), rt, true)
		if err != nil {
			t.Fatalf("runChecker failed: %v", err)
		}
	})

	t.Run("StdoutJSONOutput", func(t *testing.T) {
		dir := t.TempDir()
		p := testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		server := testutil.NewMockProxy(map[string]string{"/github.com/foo/bar/v2/@latest": "v2.0.0"})
		defer server.Close()

		var stdout bytes.Buffer
		rt := newTestRuntime(&config.Config{
			ModFilePath: p,
			JSONOutput:  true,
			MaxProbe:    2,
		}, server)
		rt.Out = &stdout

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
		p := testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

		server := testutil.NewMockProxy(map[string]string{"/github.com/foo/bar/v2/@latest": "v2.0.0"})
		defer server.Close()

		progressCalls := 0
		rt := newTestRuntime(&config.Config{
			ModFilePath: p,
			MaxProbe:    2,
		}, server)
		rt.OnProgress = func(completed, total int) {
			progressCalls++
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
