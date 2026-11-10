// Package testutil provides common test helpers across gomajor packages.
package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// RoundTripFunc allows inline functions to satisfy the http.RoundTripper interface.
type RoundTripFunc func(req *http.Request) (*http.Response, error)

// RoundTrip executes the wrapped function.
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// WriteModFile writes a go.mod file with content into dir and returns its path.
func WriteModFile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("testutil.WriteModFile: %v", err)
	}
	return p
}

// WriteConfigFile writes a configuration or data file into dir and returns its path.
func WriteConfigFile(t *testing.T, dir, filename, content string) string {
	t.Helper()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("testutil.WriteConfigFile: %v", err)
	}
	return p
}

// NewMockProxy creates an httptest.Server serving Go module proxy responses
// matching the provided URL-path to version mappings.
func NewMockProxy(versions map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if ver, ok := versions[req.URL.Path]; ok {
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": ver})
			return
		}
		rw.WriteHeader(http.StatusNotFound)
	}))
}
