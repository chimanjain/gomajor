package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

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

func setupMockProxy(_ *testing.T) (*httptest.Server, *config.Config) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			return
		}
		rw.WriteHeader(http.StatusNotFound)
	}))

	cfg := config.DefaultConfig()
	cfg.Client = checker.NewClient(
		checker.WithHTTPClient(server.Client()),
		checker.WithProxyURLs([]string{server.URL}),
	)
	cfg.MaxProbe = 2

	return server, cfg
}

func TestCheckDependencies(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		server, cfg := setupMockProxy(t)
		defer server.Close()

		eng := New(cfg)

		reqs := []*modfile.Require{
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.0.0"}},
			{Mod: module.Version{Path: "github.com/unknown/pkg", Version: "v1.0.0"}},
		}

		results, err := eng.CheckDependencies(context.Background(), reqs)
		if err != nil {
			t.Fatalf("CheckDependencies failed: %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 results, got %d", len(results))
		}

		if results[0].Current != "github.com/foo/bar" || !results[0].HasUpdate || results[0].LatestMajorVersion != "v2.0.0" {
			t.Errorf("Unexpected result for github.com/foo/bar: %+v", results[0])
		}

		if results[1].Current != "github.com/unknown/pkg" || results[1].HasUpdate {
			t.Errorf("Unexpected result for github.com/unknown/pkg: %+v", results[1])
		}
	})

	t.Run("Deduplication", func(t *testing.T) {
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

		cfg := config.DefaultConfig()
		cfg.Client = checker.NewClient(
			checker.WithHTTPClient(server.Client()),
			checker.WithProxyURLs([]string{server.URL}),
		)
		cfg.MaxProbe = 2

		eng := New(cfg)

		reqs := []*modfile.Require{
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.0.0"}},
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.1.0"}},
			{Mod: module.Version{Path: "github.com/foo/bar", Version: "v1.0.0"}},
		}

		results, err := eng.CheckDependencies(context.Background(), reqs)
		if err != nil {
			t.Fatalf("CheckDependencies failed: %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("Expected 3 results, got %d", len(results))
		}

		if results[0].CurrentVersion != "v1.0.0" || !results[0].HasUpdate || results[0].LatestMajorVersion != "v2.0.0" {
			t.Errorf("Unexpected result at index 0: %+v", results[0])
		}
		if results[1].CurrentVersion != "v1.1.0" || !results[1].HasUpdate || results[1].LatestMajorVersion != "v2.0.0" {
			t.Errorf("Unexpected result at index 1: %+v", results[1])
		}
		if results[2].CurrentVersion != "v1.0.0" || !results[2].HasUpdate || results[2].LatestMajorVersion != "v2.0.0" {
			t.Errorf("Unexpected result at index 2: %+v", results[2])
		}

		if requestCount.Load() != 1 {
			t.Errorf("Expected exactly 1 request to mock proxy for v2 update probe, got %d", requestCount.Load())
		}
	})
}

func TestRunLocalSource(t *testing.T) {
	server, cfg := setupMockProxy(t)
	defer server.Close()

	dir := t.TempDir()
	modPath := filepath.Join(dir, "go.mod")
	err := os.WriteFile(modPath, []byte("module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	eng := New(cfg)
	results, err := eng.RunLocalSource(context.Background(), modPath)
	if err != nil {
		t.Fatalf("RunLocalSource failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 SourceResult, got %d", len(results))
	}

	res := results[0]
	if res.Source != modPath || res.SourceType != source.Local {
		t.Errorf("Unexpected source info: %s (%s)", res.Source, res.SourceType)
	}

	if len(res.Dependencies) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(res.Dependencies))
	}

	dep := res.Dependencies[0]
	if dep.Module != "github.com/foo/bar" || dep.LatestMajorVersion != "v2.0.0" || !dep.HasUpdate {
		t.Errorf("Unexpected dependency info: %+v", dep)
	}
}

func TestRunMultiSources(t *testing.T) {
	server, cfg := setupMockProxy(t)
	defer server.Close()

	dir := t.TempDir()
	modPath := filepath.Join(dir, "go.mod")
	err := os.WriteFile(modPath, []byte("module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	eng := New(cfg)
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
	eng := New(config.DefaultConfig())
	_, err := eng.RunMultiSources(context.Background(), config.YAMLConfig{})
	if err == nil {
		t.Error("Expected error for empty sources, got nil")
	}
}
