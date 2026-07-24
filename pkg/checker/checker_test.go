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
			wantURLs  []string
		}{
			{"Default", "", "https://proxy.golang.org", []string{"https://proxy.golang.org"}},
			{"Custom", "https://myproxy.com", "https://myproxy.com", []string{"https://myproxy.com"}},
			{"List", "https://proxy1.com,https://proxy2.com,direct", "https://proxy1.com", []string{"https://proxy1.com", "https://proxy2.com"}},
			{"Direct", "direct", "https://proxy.golang.org", []string{"https://proxy.golang.org"}},
			{"Off", "off", "https://proxy.golang.org", []string{"https://proxy.golang.org"}},
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
				if len(c.ProxyURLs) != len(tt.wantURLs) {
					t.Fatalf("DefaultClient() ProxyURLs length = %d, want %d", len(c.ProxyURLs), len(tt.wantURLs))
				}
				for i, u := range c.ProxyURLs {
					if u != tt.wantURLs[i] {
						t.Errorf("DefaultClient() ProxyURLs[%d] = %s, want %s", i, u, tt.wantURLs[i])
					}
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
		case "/github.com/gap/mod/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.0.0"})
		case "/github.com/gap/mod/v3/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v3.0.0"})
		case "/github.com/!masterminds/semver/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.5.0"})
		case "/github.com/!masterminds/semver/v3/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v3.3.0"})
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
				name:            "MastermindsSemverMajorGapUpdate",
				modPath:         "github.com/Masterminds/semver",
				version:         "v1.5.0",
				wantUpdate:      true,
				wantMajor:       3,
				wantMajorPath:   "github.com/Masterminds/semver/v3",
				wantMajorVer:    "v3.3.0",
				wantMinorUpdate: false,
			},
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
			{
				name:            "IncompatibleMajorUpdate",
				modPath:         "github.com/foo/bar",
				version:         "v2.0.0+incompatible",
				wantUpdate:      true,
				wantMajor:       3,
				wantMajorPath:   "github.com/foo/bar/v3",
				wantMajorVer:    "v3.1.0",
				wantMinorUpdate: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := &Client{
					HTTPClient: server.Client(),
					ProxyBase:  server.URL,

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
		if major != 3 {
			t.Errorf("Expected major 3, got %d", major)
		}
		if path != "github.com/foo/bar/v3" {
			t.Errorf("Expected path github.com/foo/bar/v3, got %s", path)
		}
		if ver != "v3.1.0" {
			t.Errorf("Expected version v3.1.0, got %s", ver)
		}

		// Test that gaps in major versions (e.g. Masterminds/semver jumping v1->v3) are probed up to maxProbe
		majorGap, pathGap, verGap := client.FindLatestMajor(context.Background(), "github.com/gap/mod", 1, 5, "/")
		if majorGap != 3 {
			t.Errorf("Expected major 3 (probing across missing v2), got %d", majorGap)
		}
		if pathGap != "github.com/gap/mod/v3" {
			t.Errorf("Expected path github.com/gap/mod/v3, got %s", pathGap)
		}
		if verGap != "v3.0.0" {
			t.Errorf("Expected version v3.0.0, got %s", verGap)
		}
	})

	t.Run("FindLatestMajor_ConcurrentProbing", func(t *testing.T) {
		var hits []string
		var mu sync.Mutex
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			mu.Lock()
			hits = append(hits, req.URL.Path)
			mu.Unlock()

			switch req.URL.Path {
			case "/github.com/foo/bar/v2/@latest":
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
			case "/github.com/foo/bar/v3/@latest":
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v3.0.0"})
			case "/github.com/foo/bar/v4/@latest":
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v4.0.0"})
			default:
				rw.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		t.Run("NoUpgradeProbesMaxProbeRequests", func(t *testing.T) {
			mu.Lock()
			hits = nil
			mu.Unlock()

			client := &Client{
				HTTPClient: server.Client(),
				ProxyBase:  server.URL,
			}
			// When checking a package with no upgrades starting from v5 with maxProbe=5
			client.FindLatestMajor(context.Background(), "github.com/foo/bar", 5, 5, "/")

			mu.Lock()
			totalHits := len(hits)
			mu.Unlock()

			if totalHits != 5 {
				t.Errorf("Expected 5 requests (for v6..v10), got %d: %v", totalHits, hits)
			}
		})

		t.Run("HasUpgradeConcurrentlyProbes", func(t *testing.T) {
			mu.Lock()
			hits = nil
			mu.Unlock()

			client := &Client{
				HTTPClient: server.Client(),
				ProxyBase:  server.URL,
			}
			// Starting at v1, maxProbe=5.
			// v2, v3, v4 exist. v5 does not.
			major, path, ver := client.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 5, "/")

			if major != 4 {
				t.Errorf("Expected latest major 4, got %d", major)
			}
			if path != "github.com/foo/bar/v4" {
				t.Errorf("Expected path github.com/foo/bar/v4, got %s", path)
			}
			if ver != "v4.0.0" {
				t.Errorf("Expected version v4.0.0, got %s", ver)
			}

			// We check that v2, v3, v4, v5, v6 were queried, but v7 was NOT.
			mu.Lock()
			queryMap := make(map[string]bool)
			for _, h := range hits {
				queryMap[h] = true
			}
			mu.Unlock()

			expectedQueries := []string{
				"/github.com/foo/bar/v2/@latest",
				"/github.com/foo/bar/v3/@latest",
				"/github.com/foo/bar/v4/@latest",
				"/github.com/foo/bar/v5/@latest",
				"/github.com/foo/bar/v6/@latest",
			}
			for _, q := range expectedQueries {
				if !queryMap[q] {
					t.Errorf("Expected query to %s, but it was not made", q)
				}
			}
			if queryMap["/github.com/foo/bar/v7/@latest"] {
				t.Errorf("Unexpected query to v7 was made beyond maxProbe")
			}
		})
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
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
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
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
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

	t.Run("Singleflight_ContextCancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
		}))
		defer server.Close()

		client := &Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
		}

		ctx1, cancel1 := context.WithCancel(context.Background())
		ctx2 := context.Background()

		var wg sync.WaitGroup
		wg.Add(2)

		var ver1, ver2 string
		var ok1, ok2 bool

		// First caller starts, then gets cancelled quickly
		go func() {
			defer wg.Done()
			ver1, ok1 = client.latestVersion(ctx1, "github.com/foo/bar/v2")
		}()

		// Give first caller a tiny head start to initiate the singleflight
		time.Sleep(10 * time.Millisecond)

		// Second caller starts with standard context
		go func() {
			defer wg.Done()
			ver2, ok2 = client.latestVersion(ctx2, "github.com/foo/bar/v2")
		}()

		// Cancel the first caller's context
		time.Sleep(10 * time.Millisecond)
		cancel1()

		wg.Wait()

		// First caller should have failed/aborted due to context cancellation
		if ok1 {
			t.Errorf("Expected first caller to fail due to context cancellation, but succeeded with %s", ver1)
		}

		// Second caller should have succeeded because the underlying request was detached from the first caller's context
		if !ok2 || ver2 != "v2.0.0" {
			t.Errorf("Expected second caller to succeed with v2.0.0, but got ver=%s, ok=%t", ver2, ok2)
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
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			attempts++
			if attempts < 2 {
				rw.WriteHeader(http.StatusBadGateway) // 502 Bad Gateway
				return
			}
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.2.3"})
		}))
		defer server.Close()

		client := &Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
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
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			attempts++
			rw.WriteHeader(http.StatusNotFound) // 404
		}))
		defer server.Close()

		client := &Client{
			HTTPClient: server.Client(),
			ProxyBase:  server.URL,
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

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
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
		HTTPClient: server.Client(),
		ProxyBase:  server.URL,

		sem: make(chan struct{}, 3),
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

func TestPackageCheck(t *testing.T) {
	ctx := context.Background()
	info := Check(ctx, "github.com/foo/bar", "v1.0.0", 0)
	if info.Current != "github.com/foo/bar" {
		t.Errorf("expected current path github.com/foo/bar, got %s", info.Current)
	}
}

func TestNewClient(t *testing.T) {
	t.Run("Defaults", func(t *testing.T) {
		c := NewClient()
		if c.HTTPClient == nil {
			t.Error("HTTPClient should not be nil")
		}
		if c.ProxyBase != "https://proxy.golang.org" {
			t.Errorf("ProxyBase = %q, want default proxy", c.ProxyBase)
		}
		if len(c.ProxyURLs) != 1 || c.ProxyURLs[0] != "https://proxy.golang.org" {
			t.Errorf("ProxyURLs = %v, want [https://proxy.golang.org]", c.ProxyURLs)
		}
	})

	t.Run("WithOptions", func(t *testing.T) {
		customClient := &http.Client{Timeout: 5 * time.Second}
		urls := []string{"https://proxy1.com", "https://proxy2.com"}

		c := NewClient(
			WithHTTPClient(customClient),
			WithProxyURLs(urls),
			WithDisableMinor(true),
			WithDisableMajor(true),
			WithConcurrencyLimit(5),
		)

		if c.HTTPClient != customClient {
			t.Error("HTTPClient not set by WithHTTPClient")
		}
		if c.ProxyBase != "https://proxy1.com" {
			t.Errorf("ProxyBase = %q, want https://proxy1.com", c.ProxyBase)
		}
		if len(c.ProxyURLs) != 2 {
			t.Errorf("ProxyURLs length = %d, want 2", len(c.ProxyURLs))
		}
		if !c.DisableMinor {
			t.Error("DisableMinor should be true")
		}
		if !c.DisableMajor {
			t.Error("DisableMajor should be true")
		}
	})

	t.Run("WithConcurrencyLimit_Zero", func(t *testing.T) {
		c := NewClient(WithConcurrencyLimit(0))
		// Should keep the default semaphore, not create a zero-capacity one.
		if cap(c.sem) == 0 {
			t.Error("sem capacity should not be 0")
		}
	})
}
