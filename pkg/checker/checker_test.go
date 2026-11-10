package checker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chimanjain/gomajor/internal/testutil"
)

func newTestProxy(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
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
		case "/github.com/gone/mod/@latest":
			rw.WriteHeader(http.StatusGone)
		case "/badjson/@latest":
			_, _ = rw.Write([]byte(`{"Version":`))
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, &Client{HTTPClient: server.Client(), ProxyBase: server.URL}
}

func TestCheck(t *testing.T) {
	server, client := newTestProxy(t)
	defer server.Close()

	t.Run("StatusGoneTreatedAsNotFound", func(t *testing.T) {
		info := client.Check(context.Background(), "github.com/gone/mod", "v1.0.0", 2)
		if info.HasUpdate || info.HasMinorUpdate {
			t.Errorf("expected no update for 410 Gone module, got %+v", info)
		}
	})

	t.Run("CheckAndDisableFlags", func(t *testing.T) {
		tests := []struct {
			name         string
			modPath      string
			version      string
			disableMinor bool
			disableMajor bool
			wantUpdate   bool
			wantMajor    int
			wantPath     string
			wantMajorVer string
			wantMinor    bool
			wantMinorVer string
		}{
			{
				name:         "MastermindsSemverMajorGapUpdate",
				modPath:      "github.com/Masterminds/semver",
				version:      "v1.5.0",
				wantUpdate:   true,
				wantMajor:    3,
				wantPath:     "github.com/Masterminds/semver/v3",
				wantMajorVer: "v3.3.0",
			},
			{
				name:         "StandardMajorUpdate",
				modPath:      "github.com/foo/bar/v2",
				version:      "v2.0.0",
				wantUpdate:   true,
				wantMajor:    3,
				wantPath:     "github.com/foo/bar/v3",
				wantMajorVer: "v3.1.0",
			},
			{
				name:         "DisableMinorActive",
				modPath:      "github.com/foo/bar",
				version:      "v1.0.0",
				disableMinor: true,
				wantUpdate:   true,
				wantMajor:    3,
				wantPath:     "github.com/foo/bar/v3",
				wantMajorVer: "v3.1.0",
			},
			{
				name:         "DisableMajorActive",
				modPath:      "github.com/foo/bar",
				version:      "v1.0.0",
				disableMajor: true,
				wantMajor:    1,
				wantPath:     "github.com/foo/bar",
				wantMinor:    true,
				wantMinorVer: "v1.5.0",
			},
			{
				name:         "IncompatibleMajorUpdate",
				modPath:      "github.com/foo/bar",
				version:      "v2.0.0+incompatible",
				wantUpdate:   true,
				wantMajor:    3,
				wantPath:     "github.com/foo/bar/v3",
				wantMajorVer: "v3.1.0",
			},
			{
				name:         "DisableBothMinorAndMajorActive",
				modPath:      "github.com/foo/bar",
				version:      "v1.0.0",
				disableMinor: true,
				disableMajor: true,
				wantMajor:    1,
				wantPath:     "github.com/foo/bar",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c := &Client{
					HTTPClient:   server.Client(),
					ProxyBase:    server.URL,
					DisableMinor: tt.disableMinor,
					DisableMajor: tt.disableMajor,
				}
				info := c.Check(context.Background(), tt.modPath, tt.version, 5)
				if info.HasUpdate != tt.wantUpdate || info.LatestMajor != tt.wantMajor ||
					info.LatestMajorPath != tt.wantPath || info.LatestMajorVersion != tt.wantMajorVer ||
					info.HasMinorUpdate != tt.wantMinor || info.LatestMinorVersion != tt.wantMinorVer {
					t.Errorf("Check() mismatch: got %+v", info)
				}
			})
		}
	})

	t.Run("LatestVersionErrors", func(t *testing.T) {
		for _, path := range []string{"github.com/nonexistent", "badjson"} {
			if _, ok := client.latestVersion(context.Background(), path); ok {
				t.Errorf("expected latestVersion to fail for %s", path)
			}
		}
	})

	t.Run("FindLatestMajor", func(t *testing.T) {
		major, path, ver := client.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 5, "/")
		if major != 3 || path != "github.com/foo/bar/v3" || ver != "v3.1.0" {
			t.Errorf("FindLatestMajor expected (3, github.com/foo/bar/v3, v3.1.0), got (%d, %s, %s)", major, path, ver)
		}

		majorGap, pathGap, verGap := client.FindLatestMajor(context.Background(), "github.com/gap/mod", 1, 5, "/")
		if majorGap != 3 || pathGap != "github.com/gap/mod/v3" || verGap != "v3.0.0" {
			t.Errorf("FindLatestMajor expected gap probe (3, github.com/gap/mod/v3, v3.0.0), got (%d, %s, %s)", majorGap, pathGap, verGap)
		}
	})
}

func TestFindLatestMajor_ConcurrentProbing(t *testing.T) {
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

	client := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}

	t.Run("NoUpgradeProbesMaxProbeRequests", func(t *testing.T) {
		mu.Lock()
		hits = nil
		mu.Unlock()

		client.FindLatestMajor(context.Background(), "github.com/foo/bar", 5, 5, "/")

		mu.Lock()
		count := len(hits)
		mu.Unlock()

		if count != 5 {
			t.Errorf("Expected 5 requests, got %d", count)
		}
	})

	t.Run("HasUpgradeConcurrentlyProbes", func(t *testing.T) {
		mu.Lock()
		hits = nil
		mu.Unlock()

		freshClient := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}
		major, path, ver := freshClient.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 5, "/")
		if major != 4 || path != "github.com/foo/bar/v4" || ver != "v4.0.0" {
			t.Errorf("Expected latest major 4, got (%d, %s, %s)", major, path, ver)
		}

		mu.Lock()
		defer mu.Unlock()
		queryMap := make(map[string]bool, len(hits))
		for _, h := range hits {
			queryMap[h] = true
		}
		for _, q := range []string{
			"/github.com/foo/bar/v2/@latest",
			"/github.com/foo/bar/v3/@latest",
			"/github.com/foo/bar/v4/@latest",
			"/github.com/foo/bar/v5/@latest",
			"/github.com/foo/bar/v6/@latest",
		} {
			if !queryMap[q] {
				t.Errorf("expected query to %s was not made", q)
			}
		}
		if queryMap["/github.com/foo/bar/v7/@latest"] {
			t.Errorf("unexpected query to v7 beyond maxProbe")
		}
	})
}

func TestClient_Cache(t *testing.T) {
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

	client := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}

	// First request: Cache miss
	ver, ok := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
	if !ok || ver != "v2.0.0" || hits != 1 {
		t.Fatalf("expected v2.0.0 on miss with hits=1, got ver=%s, hits=%d", ver, hits)
	}

	// Second request: Cache hit
	ver2, ok2 := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
	if !ok2 || ver2 != "v2.0.0" || hits != 1 {
		t.Fatalf("expected v2.0.0 on hit with hits=1, got ver=%s, hits=%d", ver2, hits)
	}

	// Negative caching
	_, ok3 := client.latestVersion(context.Background(), "github.com/nonexistent")
	if ok3 || hits != 2 {
		t.Fatalf("expected negative cache miss with hits=2, got hits=%d", hits)
	}
	_, ok4 := client.latestVersion(context.Background(), "github.com/nonexistent")
	if ok4 || hits != 2 {
		t.Fatalf("expected negative cache hit with hits=2, got hits=%d", hits)
	}
}

func TestClient_Singleflight(t *testing.T) {
	var hits int
	var mu sync.Mutex

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

	client := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}

	const concurrentRequests = 10
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	for range concurrentRequests {
		go func() {
			defer wg.Done()
			ver, ok := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
			if !ok || ver != "v2.0.0" {
				t.Errorf("got ver=%s, ok=%t, want v2.0.0", ver, ok)
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("expected 1 hit due to singleflight, got %d", hits)
	}
}

func TestClient_Singleflight_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2 := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)

	var ok1, ok2 bool
	var ver2 string

	go func() {
		defer wg.Done()
		_, ok1 = client.latestVersion(ctx1, "github.com/foo/bar/v2")
	}()

	time.Sleep(10 * time.Millisecond)

	go func() {
		defer wg.Done()
		ver2, ok2 = client.latestVersion(ctx2, "github.com/foo/bar/v2")
	}()

	time.Sleep(10 * time.Millisecond)
	cancel1()

	wg.Wait()

	if ok1 {
		t.Error("expected caller 1 to fail due to cancellation")
	}
	if !ok2 || ver2 != "v2.0.0" {
		t.Errorf("expected caller 2 to succeed with v2.0.0, got (%s, %t)", ver2, ok2)
	}
}

func TestFindLatestMajor_ProbingLimits(t *testing.T) {
	t.Run("ZeroMaxProbe", func(t *testing.T) {
		c := NewClient()
		major, path, ver := c.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 0, "/")
		if major != 1 || path != "github.com/foo/bar" || ver != "" {
			t.Errorf("zero maxProbe returned unexpected (%d, %s, %s)", major, path, ver)
		}
	})

	t.Run("SingleMaxProbe", func(t *testing.T) {
		server := testutil.NewMockProxy(map[string]string{
			"/github.com/foo/bar/v2/@latest": "v2.5.0",
		})
		defer server.Close()
		c := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}

		major, path, ver := c.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 1, "/")
		if major != 2 || path != "github.com/foo/bar/v2" || ver != "v2.5.0" {
			t.Errorf("single probe found returned unexpected (%d, %s, %s)", major, path, ver)
		}

		major, path, ver = c.FindLatestMajor(context.Background(), "github.com/unknown/mod", 1, 1, "/")
		if major != 1 || path != "github.com/unknown/mod" || ver != "" {
			t.Errorf("single probe not found returned unexpected (%d, %s, %s)", major, path, ver)
		}
	})
}

func BenchmarkFindLatestMajor(b *testing.B) {
	server := testutil.NewMockProxy(map[string]string{
		"/github.com/foo/bar/v2/@latest": "v2.0.0",
		"/github.com/foo/bar/v3/@latest": "v3.0.0",
	})
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}

	for b.Loop() {
		_, _, _ = client.FindLatestMajor(context.Background(), "github.com/foo/bar", 1, 5, "/")
	}
}
