package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	t.Run("DefaultClient", func(t *testing.T) {
		tests := []struct {
			name      string
			goproxy   string
			wantProxy string
		}{
			{"Default", "", "https://proxy.golang.org"},
			{"Custom", "https://myproxy.com", "https://myproxy.com"},
			{"List", "https://proxy1.com,https://proxy2.com,direct", "https://proxy1.com"},
			{"Direct", "direct", "https://proxy.golang.org"},
			{"Off", "off", "https://proxy.golang.org"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.goproxy != "" {
					t.Setenv("GOPROXY", tt.goproxy)
				} else {
					_ = os.Unsetenv("GOPROXY")
				}
				c := DefaultClient()
				if c.ProxyBase != tt.wantProxy {
					t.Errorf("DefaultClient() ProxyBase = %s, want %s", c.ProxyBase, tt.wantProxy)
				}
			})
		}
	})

	// Reusable unified Mock Proxy Server for functional subtests
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/github.com/foo/bar/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.5.0"})
		case "/github.com/foo/bar/v2/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		case "/github.com/foo/bar/v3/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v3.1.0"})
		case "/badjson/@latest":
			_, _ = rw.Write([]byte(`{"Version":`)) // malformed JSON
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("CheckAndDisableFlags", func(t *testing.T) {
		tests := []struct {
			name            string
			modPath         string
			version         string
			disableMinor    bool
			disableMajor    bool
			wantUpdate      bool
			wantMajor       int
			wantMajorPath   string
			wantMajorVer    string
			wantMinorUpdate bool
			wantMinorVer    string
		}{
			{
				name:            "StandardMajorUpdate",
				modPath:         "github.com/foo/bar/v2",
				version:         "v2.0.0",
				wantUpdate:      true,
				wantMajor:       3,
				wantMajorPath:   "github.com/foo/bar/v3",
				wantMajorVer:    "v3.1.0",
				wantMinorUpdate: false,
			},
			{
				name:            "DisableMinorActive",
				modPath:         "github.com/foo/bar",
				version:         "v1.0.0",
				disableMinor:    true,
				wantUpdate:      true,
				wantMajor:       3,
				wantMajorPath:   "github.com/foo/bar/v3",
				wantMajorVer:    "v3.1.0",
				wantMinorUpdate: false,
			},
			{
				name:            "DisableMajorActive",
				modPath:         "github.com/foo/bar",
				version:         "v1.0.0",
				disableMajor:    true,
				wantUpdate:      false,
				wantMajor:       1,
				wantMajorPath:   "github.com/foo/bar",
				wantMinorUpdate: true,
				wantMinorVer:    "v1.5.0",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := &Client{
					HTTPClient:   server.Client(),
					ProxyBase:    server.URL,
					DisableMinor: tt.disableMinor,
					DisableMajor: tt.disableMajor,
				}
				info := client.Check(context.Background(), tt.modPath, tt.version, 5)

				if info.HasUpdate != tt.wantUpdate {
					t.Errorf("HasUpdate = %t, want %t", info.HasUpdate, tt.wantUpdate)
				}
				if info.LatestMajor != tt.wantMajor {
					t.Errorf("LatestMajor = %d, want %d", info.LatestMajor, tt.wantMajor)
				}
				if info.LatestMajorPath != tt.wantMajorPath {
					t.Errorf("LatestMajorPath = %q, want %q", info.LatestMajorPath, tt.wantMajorPath)
				}
				if info.LatestMajorVersion != tt.wantMajorVer {
					t.Errorf("LatestMajorVersion = %q, want %q", info.LatestMajorVersion, tt.wantMajorVer)
				}
				if info.HasMinorUpdate != tt.wantMinorUpdate {
					t.Errorf("HasMinorUpdate = %t, want %t", info.HasMinorUpdate, tt.wantMinorUpdate)
				}
				if info.LatestMinorVersion != tt.wantMinorVer {
					t.Errorf("LatestMinorVersion = %q, want %q", info.LatestMinorVersion, tt.wantMinorVer)
				}
			})
		}
	})

	t.Run("LatestVersionError", func(t *testing.T) {
		client := &Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		}

		tests := []struct {
			name    string
			modPath string
		}{
			{"ServerError500", "github.com/nonexistent"},
			{"MalformedJSON", "badjson"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, ok := client.latestVersion(context.Background(), tt.modPath); ok {
					t.Errorf("Expected latestVersion to fail for %s", tt.modPath)
				}
			})
		}
	})

	t.Run("FindLatestMajor", func(t *testing.T) {
		client := &Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		}

		major, path, ver := client.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 5, "/")
		if major != 2 {
			t.Errorf("Expected major 2, got %d", major)
		}
		if path != "github.com/foo/bar/v2" {
			t.Errorf("Expected path github.com/foo/bar/v2, got %s", path)
		}
		if ver != "v2.0.0" {
			t.Errorf("Expected version v2.0.0, got %s", ver)
		}
	})

	t.Run("Cache", func(t *testing.T) {
		var hits int
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			hits++
			if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			} else {
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := &Client{
			HTTPClient:  server.Client(),
			ProxyBase:   server.URL,
			latestCache: make(map[string]string),
		}

		// First request: Cache miss, hits should be 1
		ver, ok := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
		if !ok || ver != "v2.0.0" {
			t.Fatalf("Expected v2.0.0, got %s (ok=%t)", ver, ok)
		}
		if hits != 1 {
			t.Errorf("Expected 1 hit to server, got %d", hits)
		}

		// Second request: Cache hit, hits should still be 1
		ver2, ok2 := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
		if !ok2 || ver2 != "v2.0.0" {
			t.Fatalf("Expected v2.0.0 on second call, got %s (ok=%t)", ver2, ok2)
		}
		if hits != 1 {
			t.Errorf("Expected hits to remain 1 due to cache, got %d", hits)
		}

		// Third request for a nonexistent path: Cache miss on negative result, hits should be 2
		_, ok3 := client.latestVersion(context.Background(), "github.com/nonexistent")
		if ok3 {
			t.Fatalf("Expected path to be nonexistent")
		}
		if hits != 2 {
			t.Errorf("Expected 2 hits to server, got %d", hits)
		}

		// Fourth request for nonexistent path: Cache hit (negative caching), hits should still be 2
		_, ok4 := client.latestVersion(context.Background(), "github.com/nonexistent")
		if ok4 {
			t.Fatalf("Expected path to be nonexistent on cache hit")
		}
		if hits != 2 {
			t.Errorf("Expected hits to remain 2 due to negative caching, got %d", hits)
		}
	})

	t.Run("Singleflight", func(t *testing.T) {
		var hits int
		var mu sync.Mutex

		// A delay is introduced inside the server to ensure concurrent client requests
		// are in flight at the same time, allowing coalescing to take place.
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			mu.Lock()
			hits++
			mu.Unlock()

			time.Sleep(100 * time.Millisecond)
			if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			} else {
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := &Client{
			HTTPClient:  server.Client(),
			ProxyBase:   server.URL,
			latestCache: make(map[string]string),
		}

		const concurrentRequests = 10
		var wg sync.WaitGroup
		wg.Add(concurrentRequests)

		results := make([]string, concurrentRequests)
		successes := make([]bool, concurrentRequests)

		for i := range concurrentRequests {
			go func(index int) {
				defer wg.Done()
				ver, ok := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
				results[index] = ver
				successes[index] = ok
			}(i)
		}

		wg.Wait()

		// Assert all concurrent requests finished successfully and fetched the correct value
		for i := range concurrentRequests {
			if !successes[i] || results[i] != "v2.0.0" {
				t.Errorf("Request %d failed: got %s (ok=%t), expected v2.0.0", i, results[i], successes[i])
			}
		}

		// Assert singleflight coalesced duplicate calls so that only a single request was sent to the server
		mu.Lock()
		actualHits := hits
		mu.Unlock()
		if actualHits != 1 {
			t.Errorf("Expected exactly 1 proxy hit due to singleflight coalescing, got %d", actualHits)
		}
	})

	t.Run("GoPrivate", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if strings.Contains(req.URL.Path, "private") {
				t.Errorf("Server should not be hit for private modules, but got request to %s", req.URL.Path)
			}
			rw.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := &Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		}

		tests := []struct {
			name      string
			goprivate string
			modPath   string
			wantOk    bool
		}{
			{"WildcardMatch", "github.com/myorg/private-*,git.internal.corp", "github.com/myorg/private-repo", false},
			{"PrefixMatch", "github.com/myorg/private-*,git.internal.corp", "git.internal.corp/foo/bar", false},
			{"NoMatch", "github.com/myorg/private-*,git.internal.corp", "github.com/myorg/public-repo", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Setenv("GOPRIVATE", tt.goprivate)
				_, ok := client.latestVersion(context.Background(), tt.modPath)
				if ok != tt.wantOk {
					t.Errorf("latestVersion(%s) ok = %t, want %t", tt.modPath, ok, tt.wantOk)
				}
			})
		}
	})
}

func TestClient_Retry(t *testing.T) {
	t.Run("TransientErrorsRetried", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			attempts++
			if attempts < 2 {
				rw.WriteHeader(http.StatusBadGateway) // 502 Bad Gateway
				return
			}
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.2.3"})
		}))
		defer server.Close()

		client := &Client{
			HTTPClient:  server.Client(),
			ProxyBase:   server.URL,
			latestCache: make(map[string]string),
		}

		ver, ok := client.latestVersion(context.Background(), "github.com/foo/bar")
		if !ok || ver != "v1.2.3" {
			t.Errorf("latestVersion failed: got %s (ok=%t), expected v1.2.3", ver, ok)
		}
		if attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})

	t.Run("TerminalErrorImmediateFail", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			attempts++
			rw.WriteHeader(http.StatusNotFound) // 404
		}))
		defer server.Close()

		client := &Client{
			HTTPClient:  server.Client(),
			ProxyBase:   server.URL,
			latestCache: make(map[string]string),
		}

		_, ok := client.latestVersion(context.Background(), "github.com/foo/bar")
		if ok {
			t.Errorf("expected latestVersion to fail on 404")
		}
		if attempts != 1 {
			t.Errorf("expected exactly 1 attempt for 404 terminal error, got %d", attempts)
		}
	})
}

func TestClient_Concurrency(t *testing.T) {
	var inFlight int
	var maxInFlight int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()

		_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.0.0"})
	}))
	defer server.Close()

	client := &Client{
		HTTPClient:  server.Client(),
		ProxyBase:   server.URL,
		latestCache: make(map[string]string),
		sem:         make(chan struct{}, 3),
	}

	const totalRequests = 10
	var wg sync.WaitGroup
	wg.Add(totalRequests)

	for i := 0; i < totalRequests; i++ {
		go func(idx int) {
			defer wg.Done()
			_, _ = client.fetchLatestVersion(context.Background(), fmt.Sprintf("github.com/foo/bar%d", idx))
		}(i)
	}

	wg.Wait()

	if maxInFlight > 3 {
		t.Errorf("expected max in-flight requests to be <= 3, got %d", maxInFlight)
	}
}
