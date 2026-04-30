package checker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseModulePath(t *testing.T) {
	tests := []struct {
		name      string
		modPath   string
		wantBase  string
		wantMajor int
	}{
		{"Standard v2", "github.com/user/gomodule/v2", "github.com/user/gomodule", 2},
		{"Standard v3", "github.com/user/gomodule/v3", "github.com/user/gomodule", 3},
		{"Gopkg.in v2", "gopkg.in/yaml.v2", "gopkg.in/yaml", 2},
		{"Gopkg.in v3", "gopkg.in/yaml.v3", "gopkg.in/yaml", 3},
		{"Unversioned", "github.com/google/uuid", "github.com/google/uuid", 1},
		{"Invalid v", "github.com/foo/bar/v", "github.com/foo/bar/v", 1},
		{"Invalid v1", "github.com/foo/bar/v1", "github.com/foo/bar/v1", 1},
		{"Double digit v", "github.com/foo/bar/v10", "github.com/foo/bar", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotMajor := ParseModulePath(tt.modPath)
			if gotBase != tt.wantBase {
				t.Errorf("ParseModulePath() gotBase = %v, want %v", gotBase, tt.wantBase)
			}
			if gotMajor != tt.wantMajor {
				t.Errorf("ParseModulePath() gotMajor = %v, want %v", gotMajor, tt.wantMajor)
			}
		})
	}
}

func TestNextMajorPath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		major    int
		want     string
	}{
		{"Standard", "github.com/user/gomodule", 3, "github.com/user/gomodule/v3"},
		{"Gopkg.in", "gopkg.in/yaml", 3, "gopkg.in/yaml.v3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextMajorPath(tt.basePath, tt.major); got != tt.want {
				t.Errorf("nextMajorPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEscapePath(t *testing.T) {
	tests := []struct {
		name    string
		modPath string
		want    string
		wantErr bool
	}{
		{"No uppercase", "github.com/google/uuid", "github.com/google/uuid", false},
		{"With uppercase", "github.com/BurntSushi/toml", "github.com/!burnt!sushi/toml", false},
		{"Empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := escapePath(tt.modPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("escapePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("escapePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Mock responses based on the URL path
		switch req.URL.Path {
		case "/github.com/foo/bar/v2/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		case "/github.com/foo/bar/v3/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v3.1.0"})
		case "/github.com/foo/bar/v4/@latest":
			rw.WriteHeader(http.StatusNotFound)
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	// Close the server when test finishes
	defer server.Close()

	client := &Client{
		HTTPClient: server.Client(),
		ProxyBase:  server.URL,
	}

	info := client.Check(context.Background(), "github.com/foo/bar/v2", "v2.0.0", 5)

	if !info.HasUpdate {
		t.Errorf("Expected HasUpdate to be true, got false")
	}
	if info.LatestMajor != 3 {
		t.Errorf("Expected LatestMajor to be 3, got %d", info.LatestMajor)
	}
	if info.LatestMajorPath != "github.com/foo/bar/v3" {
		t.Errorf("Expected LatestMajorPath to be github.com/foo/bar/v3, got %s", info.LatestMajorPath)
	}
	if info.LatestMajorVersion != "v3.1.0" {
		t.Errorf("Expected LatestMajorVersion to be v3.1.0, got %s", info.LatestMajorVersion)
	}
}

func TestLatestVersion_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/badjson/@latest" {
			_, _ = rw.Write([]byte(`{"Version":`)) // truncated json
			return
		}
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{
		HTTPClient: server.Client(),
		ProxyBase:  server.URL,
	}

	if _, ok := client.latestVersion(context.Background(), "github.com/nonexistent"); ok {
		t.Errorf("Expected latestVersion to fail on 500")
	}

	if _, ok := client.latestVersion(context.Background(), "badjson"); ok {
		t.Errorf("Expected latestVersion to fail on bad json")
	}
}

func TestFindLatestMajor_CurrentMajor1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		} else {
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &Client{
		HTTPClient: server.Client(),
		ProxyBase:  server.URL,
	}

	major, path, ver := client.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 5)
	if major != 2 {
		t.Errorf("Expected major 2, got %d", major)
	}
	if path != "github.com/foo/bar/v2" {
		t.Errorf("Expected path github.com/foo/bar/v2, got %s", path)
	}
	if ver != "v2.0.0" {
		t.Errorf("Expected version v2.0.0, got %s", ver)
	}
}
