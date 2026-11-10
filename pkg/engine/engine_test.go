package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/chimanjain/gomajor/internal/testutil"
	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/source"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

func TestNormalizeSources(t *testing.T) {
	yamlCfg := &config.YAMLConfig{
		Local:  []string{"path/to/local", "path/to/local"},
		Github: []string{"owner/repo1", "owner/repo1, owner/repo2\towner/repo3"},
	}

	normalizeSources(yamlCfg)

	wantLocal := []string{"path/to/local"}
	if !reflect.DeepEqual(yamlCfg.Local, wantLocal) {
		t.Errorf("yamlCfg.Local = %v, want %v", yamlCfg.Local, wantLocal)
	}

	wantGithub := []string{"owner/repo1", "owner/repo2", "owner/repo3"}
	if !reflect.DeepEqual(yamlCfg.Github, wantGithub) {
		t.Errorf("yamlCfg.Github = %v, want %v", yamlCfg.Github, wantGithub)
	}
}

func TestRun(t *testing.T) {
	server, opts := setupMockProxy(t)
	defer server.Close()

	dir := t.TempDir()
	modPath := testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

	eng := New(opts)
	providers := []source.Provider{source.NewLocalProvider(modPath)}

	results, err := eng.Run(context.Background(), providers)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 SourceResult, got %d", len(results))
	}
	if len(results[0].Dependencies) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(results[0].Dependencies))
	}
	if results[0].Dependencies[0].LatestMajorVersion != "v2.0.0" {
		t.Errorf("Expected v2.0.0, got %s", results[0].Dependencies[0].LatestMajorVersion)
	}
}

func TestRunMultiSources(t *testing.T) {
	server, opts := setupMockProxy(t)
	defer server.Close()

	dir := t.TempDir()
	modPath := testutil.WriteModFile(t, dir, "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n")

	eng := New(opts)
	yamlCfg := config.YAMLConfig{
		Local: []string{modPath},
	}

	results, err := eng.RunMultiSources(context.Background(), yamlCfg)
	if err != nil {
		t.Fatalf("RunMultiSources failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 SourceResult, got %d", len(results))
	}
}

func TestRunMultiSources_Empty(t *testing.T) {
	eng := New(Options{})
	_, err := eng.RunMultiSources(context.Background(), config.YAMLConfig{})
	if err == nil {
		t.Error("Expected error for empty sources, got nil")
	}
}

func setupMockProxy(_ *testing.T) (*httptest.Server, Options) {
	server := testutil.NewMockProxy(map[string]string{
		"/github.com/foo/bar/v2/@latest": "v2.0.0",
	})

	opts := Options{
		Client: checker.NewClient(
			checker.WithHTTPClient(server.Client()),
			checker.WithProxyURLs([]string{server.URL}),
		),
		MaxProbe: 2,
	}

	return server, opts
}

type errProvider struct {
	name string
}

func (e errProvider) Name() string      { return e.name }
func (e errProvider) Type() source.Type { return source.Local }
func (e errProvider) Parse(_ context.Context, _ *http.Client) (source.ParsedSource, error) {
	return source.ParsedSource{}, errors.New("provider error")
}

func TestParseAllProviders_ErrorHandling(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	eng := New(Options{Logger: logger})

	providers := []source.Provider{
		errProvider{name: "failing-provider"},
	}

	sources, err := eng.parseAllProviders(context.Background(), providers)
	if err != nil {
		t.Fatalf("unexpected error from parseAllProviders: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sources))
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("failed to parse source")) {
		t.Errorf("expected log warning for failing provider, got: %s", logBuf.String())
	}
}

func TestRunMultiSources_GitHubDefaultClient(t *testing.T) {
	eng := New(Options{})
	if eng.opts.GitHubHTTPClient == nil {
		t.Fatal("expected GitHubHTTPClient to be defaulted, got nil")
	}
}

func TestRunParsedSources(t *testing.T) {
	server, opts := setupMockProxy(t)
	defer server.Close()

	eng := New(opts)
	parsed := source.ParsedSource{
		Source:     "test/go.mod",
		SourceType: source.Local,
		Reqs: []*modfile.Require{
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.0.0"}},
		},
	}

	results, err := eng.RunParsedSources(context.Background(), []source.ParsedSource{parsed})
	if err != nil {
		t.Fatalf("RunParsedSources failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Dependencies) != 1 {
		t.Fatalf("expected 1 result with 1 dependency, got %+v", results)
	}
	if results[0].Dependencies[0].LatestMajorVersion != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %s", results[0].Dependencies[0].LatestMajorVersion)
	}
}

func TestRunParsedSources_Deduplication(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
			requestCount.Add(1)
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			return
		}
		rw.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	opts := Options{
		Client: checker.NewClient(
			checker.WithHTTPClient(server.Client()),
			checker.WithProxyURLs([]string{server.URL}),
		),
		MaxProbe: 2,
	}

	eng := New(opts)

	parsed := source.ParsedSource{
		Source:     "test/go.mod",
		SourceType: source.Local,
		Reqs: []*modfile.Require{
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.0.0"}},
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.1.0"}},
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.0.0"}},
		},
	}

	results, err := eng.RunParsedSources(context.Background(), []source.ParsedSource{parsed})
	if err != nil {
		t.Fatalf("RunParsedSources failed: %v", err)
	}

	if len(results) != 1 || len(results[0].Dependencies) != 3 {
		t.Fatalf("Expected 1 result with 3 deps, got %+v", results)
	}

	if requestCount.Load() != 1 {
		t.Errorf("Expected exactly 1 request to mock proxy for v2 update probe, got %d", requestCount.Load())
	}
}
