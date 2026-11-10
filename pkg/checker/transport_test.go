package checker

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/chimanjain/gomajor/pkg/config"
)

func parseTestURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("failed to parse url: %v", err)
	}
	return u
}

func TestSafeCheckRedirect(t *testing.T) {
	initial := parseTestURL(t, "https://example.com/initial")
	tenVia := make([]*http.Request, 10)
	for i := range tenVia {
		tenVia[i] = &http.Request{URL: parseTestURL(t, fmt.Sprintf("https://example.com/%d", i))}
	}

	tests := []struct {
		name      string
		targetURL string
		via       []*http.Request
		wantErr   bool
		wantAuth  string
	}{
		{"SameHostAndSchemePreservesAuth", "https://example.com/redirected", []*http.Request{{URL: initial}}, false, "Bearer secret"},
		{"CrossHostStripsAuth", "https://other.com/redirected", []*http.Request{{URL: initial}}, false, ""},
		{"SchemeDowngradeForbidden", "http://example.com/redirected", []*http.Request{{URL: initial}}, true, ""},
		{"PortChangeStripsAuth", "https://example.com:8443/redirected", []*http.Request{{URL: initial}}, false, ""},
		{"UnsupportedSchemeRejected", "file:///etc/passwd", []*http.Request{{URL: initial}}, true, ""},
		{"MaxRedirectsExceeded", "https://example.com/11", tenVia, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tt.targetURL, nil)
			req.Header.Set("Authorization", "Bearer secret")
			err := SafeCheckRedirect(req, tt.via)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SafeCheckRedirect() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && req.Header.Get("Authorization") != tt.wantAuth {
				t.Errorf("Authorization header = %q, want %q", req.Header.Get("Authorization"), tt.wantAuth)
			}
		})
	}
}

func TestDefaultTransportTLS(t *testing.T) {
	tr := defaultTransport()
	httpTr, ok := tr.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", tr)
	}
	if httpTr.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be configured, got nil")
	}
	if httpTr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected TLS MinVersion to be TLS 1.2 (0x%04x), got 0x%04x", tls.VersionTLS12, httpTr.TLSClientConfig.MinVersion)
	}
	if httpTr.ResponseHeaderTimeout != config.HTTPResponseHeaderTimeout {
		t.Errorf("expected ResponseHeaderTimeout %v, got %v", config.HTTPResponseHeaderTimeout, httpTr.ResponseHeaderTimeout)
	}
	if httpTr.TLSHandshakeTimeout != config.HTTPTLSHandshakeTimeout {
		t.Errorf("expected TLSHandshakeTimeout %v, got %v", config.HTTPTLSHandshakeTimeout, httpTr.TLSHandshakeTimeout)
	}
	if httpTr.IdleConnTimeout != config.HTTPIdleConnTimeout {
		t.Errorf("expected IdleConnTimeout %v, got %v", config.HTTPIdleConnTimeout, httpTr.IdleConnTimeout)
	}
	if httpTr.ExpectContinueTimeout != config.HTTPExpectContinueTimeout {
		t.Errorf("expected ExpectContinueTimeout %v, got %v", config.HTTPExpectContinueTimeout, httpTr.ExpectContinueTimeout)
	}
}

func TestIsSecureProxyURL(t *testing.T) {
	tests := []struct {
		url    string
		secure bool
	}{
		{"https://proxy.golang.org", true},
		{"https://goproxy.io", true},
		{"http://localhost:8080", true},
		{"http://127.0.0.1:8080", true},
		{"http://[::1]:8080", true},
		{"http://myhost.localhost:8080", true},
		{"http://proxy.golang.org", false},
		{"http://169.254.169.254/latest", false},
		{"http://evil.com", false},
		{"ftp://proxy.golang.org", false},
		{"file:///etc/passwd", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isSecureProxyURL(tt.url)
		if got != tt.secure {
			t.Errorf("isSecureProxyURL(%q) = %v, want %v", tt.url, got, tt.secure)
		}
	}
}

func TestWithProxyURLs_InsecureFiltered(t *testing.T) {
	c := NewClient(WithProxyURLs([]string{"http://evil.com", "https://valid-proxy.com"}))
	if len(c.ProxyURLs) != 1 || c.ProxyURLs[0] != "https://valid-proxy.com" {
		t.Errorf("expected insecure proxy to be filtered out, got %v", c.ProxyURLs)
	}

	// When all provided URLs are insecure, fallback to defaultProxy
	cAllInsecure := NewClient(WithProxyURLs([]string{"http://169.254.169.254"}))
	if len(cAllInsecure.ProxyURLs) != 1 || cAllInsecure.ProxyURLs[0] != defaultProxy {
		t.Errorf("expected fallback to defaultProxy, got %v", cAllInsecure.ProxyURLs)
	}
}
