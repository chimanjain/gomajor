package checker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

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

func TestDefaultClient(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		_ = os.Setenv("GOPROXY", "")
		c := DefaultClient()
		if c.ProxyBase != "https://proxy.golang.org" {
			t.Errorf("Expected https://proxy.golang.org, got %s", c.ProxyBase)
		}
		_ = os.Unsetenv("GOPROXY")
	})

	t.Run("Custom", func(t *testing.T) {
		_ = os.Setenv("GOPROXY", "https://myproxy.com")
		c := DefaultClient()
		if c.ProxyBase != "https://myproxy.com" {
			t.Errorf("Expected https://myproxy.com, got %s", c.ProxyBase)
		}
		_ = os.Unsetenv("GOPROXY")
	})

	t.Run("List", func(t *testing.T) {
		_ = os.Setenv("GOPROXY", "https://proxy1.com,https://proxy2.com,direct")
		c := DefaultClient()
		if c.ProxyBase != "https://proxy1.com" {
			t.Errorf("Expected https://proxy1.com, got %s", c.ProxyBase)
		}
		_ = os.Unsetenv("GOPROXY")
	})

	t.Run("DirectOrOff", func(t *testing.T) {
		_ = os.Setenv("GOPROXY", "direct")
		c := DefaultClient()
		if c.ProxyBase != "https://proxy.golang.org" {
			t.Errorf("Expected default proxy when direct, got %s", c.ProxyBase)
		}
		_ = os.Unsetenv("GOPROXY")
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
}

func TestCheck_DisableFlags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/github.com/foo/bar/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.5.0"})
		case "/github.com/foo/bar/v2/@latest":
			_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.1.0"})
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("DisableMinor", func(t *testing.T) {
		client := &Client{
			HTTPClient:   server.Client(),
			ProxyBase:    server.URL,
			DisableMinor: true,
		}
		info := client.Check(context.Background(), "github.com/foo/bar", "v1.0.0", 5)
		if info.HasMinorUpdate {
			t.Errorf("Expected HasMinorUpdate to be false when DisableMinor is true")
		}
		if info.LatestMinorVersion != "" {
			t.Errorf("Expected LatestMinorVersion to be empty, got %s", info.LatestMinorVersion)
		}
		if !info.HasUpdate || info.LatestMajorVersion != "v2.1.0" {
			t.Errorf("Expected major update to still work: %+v", info)
		}
	})

	t.Run("DisableMajor", func(t *testing.T) {
		client := &Client{
			HTTPClient:   server.Client(),
			ProxyBase:    server.URL,
			DisableMajor: true,
		}
		info := client.Check(context.Background(), "github.com/foo/bar", "v1.0.0", 5)
		if info.HasUpdate {
			t.Errorf("Expected HasUpdate to be false when DisableMajor is true")
		}
		if info.LatestMajorVersion != "" {
			t.Errorf("Expected LatestMajorVersion to be empty, got %s", info.LatestMajorVersion)
		}
		if !info.HasMinorUpdate || info.LatestMinorVersion != "v1.5.0" {
			t.Errorf("Expected minor update to still work: %+v", info)
		}
	})
}

func TestClient_Singleflight(t *testing.T) {
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

	for i := 0; i < concurrentRequests; i++ {
		go func(index int) {
			defer wg.Done()
			ver, ok := client.latestVersion(context.Background(), "github.com/foo/bar/v2")
			results[index] = ver
			successes[index] = ok
		}(i)
	}

	wg.Wait()

	// Assert all concurrent requests finished successfully and fetched the correct value
	for i := 0; i < concurrentRequests; i++ {
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
}


