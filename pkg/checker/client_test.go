package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

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
		if cap(c.sem) == 0 {
			t.Error("sem capacity should not be 0")
		}
	})
}

func TestDefaultClient(t *testing.T) {
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
}

func TestDefaultClient_OptionsAppliedOnce(t *testing.T) {
	callCount := 0
	opt := func(_ *Client) { callCount++ }
	_ = DefaultClient(opt)
	if callCount != 1 {
		t.Errorf("expected option to be invoked exactly once, got %d", callCount)
	}
}

func TestClient_Close(t *testing.T) {
	c := NewClient()
	c.Close()
}

func TestClient_WithLogger(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c := NewClient(WithLogger(logger))
	if c.Logger != logger {
		t.Error("Logger was not set on Client")
	}

	t.Setenv("GOPROXY", "direct")
	c2 := DefaultClient(WithLogger(logger))
	if c2.Logger != logger {
		t.Error("Logger was not set on DefaultClient")
	}
	if !strings.Contains(logBuf.String(), "GOPROXY value not supported") {
		t.Errorf("Expected fallback warning in custom logger buffer, got: %s", logBuf.String())
	}
}

func TestWithBaseContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := NewClient(WithBaseContext(ctx))
	if c.BaseContext != ctx {
		t.Errorf("expected BaseContext to be set")
	}

	cancel()
	if _, ok := c.latestVersion(context.Background(), "github.com/foo/bar"); ok {
		t.Error("expected latestVersion to fail when BaseContext is cancelled")
	}
}

func TestClient_Retry(t *testing.T) {
	tests := []struct {
		name         string
		firstStatus  int
		wantOk       bool
		wantAttempts int
	}{
		{"TransientErrorsRetried", http.StatusBadGateway, true, 2},
		{"TerminalErrorImmediateFail", http.StatusNotFound, false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				attempts++
				if attempts < 2 && tt.firstStatus != http.StatusOK {
					rw.WriteHeader(tt.firstStatus)
					return
				}
				_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v1.2.3"})
			}))
			defer server.Close()

			c := &Client{HTTPClient: server.Client(), ProxyBase: server.URL}
			ver, ok := c.latestVersion(context.Background(), "github.com/foo/bar")
			if ok != tt.wantOk {
				t.Errorf("latestVersion ok = %t, want %t", ok, tt.wantOk)
			}
			if tt.wantOk && ver != "v1.2.3" {
				t.Errorf("latestVersion ver = %q, want v1.2.3", ver)
			}
			if attempts != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", attempts, tt.wantAttempts)
			}
		})
	}
}

func TestClient_Concurrency(t *testing.T) {
	var inFlight, maxInFlight int
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
		sem:        make(chan struct{}, 3),
	}

	const totalRequests = 10
	var wg sync.WaitGroup
	wg.Add(totalRequests)

	for i := range totalRequests {
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

func TestClient_GoPrivate(t *testing.T) {
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
}

func TestClient_PrivateShortCircuit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Errorf("Proxy should never be queried for private module, got: %s", req.URL.Path)
		rw.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		HTTPClient: server.Client(),
		ProxyBase:  server.URL,
	}

	t.Setenv("GOPRIVATE", "corp.internal/*")
	info := client.Check(context.Background(), "corp.internal/secret/repo/v2", "v2.1.0", 5)
	if info.HasUpdate || info.HasMinorUpdate {
		t.Errorf("Expected no updates for private module, got: %+v", info)
	}
	if info.Current != "corp.internal/secret/repo/v2" || info.CurrentMajor != 2 {
		t.Errorf("Unexpected info for private module: %+v", info)
	}
}
