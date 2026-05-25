package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chimanjain/gomajor/checker"
	"go.yaml.in/yaml/v3"
)

func TestRunChecker_NoDirectDeps(t *testing.T) {
	dir := t.TempDir()
	// Only indirect dependencies — runChecker should print "No matching dependencies".
	content := `module example.com/test

go 1.21

require github.com/google/uuid v1.6.0 // indirect
`
	p := writeModFile(t, dir, content)
	testConfig := &Config{
		ModFilePath: p,
		CheckAll:    false,
		MaxProbe:    0, // no network probing
		Client:      checker.DefaultClient(),
	}

	// runChecker must not panic; we just verify it returns without crashing.
	err := runCheckerWithConfig(testConfig, true, false, false)
	if err != nil {
		t.Errorf("runCheckerWithConfig returned unexpected error: %v", err)
	}
}

func TestRunChecker_EmptyMod(t *testing.T) {
	dir := t.TempDir()
	content := "module example.com/empty\n\ngo 1.21\n"
	p := writeModFile(t, dir, content)
	testConfig := &Config{
		ModFilePath: p,
		CheckAll:    false,
		MaxProbe:    0,
		Client:      checker.DefaultClient(),
	}

	err := runCheckerWithConfig(testConfig, true, false, false)
	if err != nil {
		t.Errorf("runCheckerWithConfig returned unexpected error: %v", err)
	}
}

func TestRunChecker_WithUpdatesMock(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/test

go 1.21

require github.com/foo/bar v1.0.0
`
	p := writeModFile(t, dir, content)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		} else {
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	testConfig := &Config{
		ModFilePath: p,
		CheckAll:    false,
		MaxProbe:    2,
		Client: &checker.Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		},
	}

	err := runCheckerWithConfig(testConfig, true, false, false)
	if err != nil {
		t.Errorf("runCheckerWithConfig returned unexpected error: %v", err)
	}
}

func TestRunChecker_AllDeps(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/test

go 1.21

require (
	github.com/foo/bar v1.0.0
	github.com/foo/baz v1.0.0 // indirect
)
`
	p := writeModFile(t, dir, content)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	testConfig := &Config{
		ModFilePath: p,
		CheckAll:    true,
		MaxProbe:    1,
		Client: &checker.Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		},
	}

	err := runCheckerWithConfig(testConfig, true, false, false)
	if err != nil {
		t.Errorf("runCheckerWithConfig returned unexpected error: %v", err)
	}
}

func TestRunChecker_Json(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/test
require github.com/foo/bar v1.0.0
`
	p := writeModFile(t, dir, content)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		} else {
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	testConfig := &Config{
		ModFilePath: p,
		MaxProbe:    2,
		JsonOutput:  true,
		Client: &checker.Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		},
	}

	err := runCheckerWithConfig(testConfig, true, false, false)
	if err != nil {
		t.Errorf("runCheckerWithConfig returned unexpected error: %v", err)
	}
}

func TestRunMultiChecker(t *testing.T) {
	dir := t.TempDir()

	// 1. Create a local go.mod file
	localModPath := filepath.Join(dir, "local.mod")
	localContent := `module example.com/local
go 1.21
require github.com/foo/bar v1.0.0
`
	if err := os.WriteFile(localModPath, []byte(localContent), 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	// 2. Set up HTTP mock server for GitHub raw file and Go module proxies
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

	// 3. Create a config.yaml file
	configPath := filepath.Join(dir, "gomajor.yaml")
	outputPath := filepath.Join(dir, "gomajor-report.yaml")

	yamlCfg := YAMLConfig{
		Local:  []string{localModPath},
		Github: []string{server.URL + "/owner/repo/main/go.mod"},
		Output: outputPath,
	}
	yamlBytes, err := yaml.Marshal(yamlCfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	// 4. Run the multi-checker using runMultiChecker
	testConfig := &Config{
		MaxProbe: 2,
		Client: &checker.Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		},
		ConfigPath: configPath,
	}

	// Execute multi-checker (passing false for outputExplicit, because it's in the YAML)
	err = runMultiChecker(testConfig, configPath, false)
	if err != nil {
		t.Fatalf("runMultiChecker failed: %v", err)
	}

	// 5. Read the output.yaml to verify contents
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("expected output file %s does not exist", outputPath)
	}

	outContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	var output YAMLOutput
	if err := yaml.Unmarshal(outContent, &output); err != nil {
		t.Fatalf("failed to unmarshal output YAML: %v", err)
	}

	if len(output.Results) != 2 {
		t.Errorf("len(output.Results) = %d, want 2", len(output.Results))
	}

	// Check local result
	localFound := false
	for _, res := range output.Results {
		if res.Source == localModPath {
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
}
