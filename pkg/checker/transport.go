package checker

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/chimanjain/gomajor/pkg/config"
)

// isSecureProxyURL reports whether raw is a secure Go module proxy URL.
// It requires the HTTPS scheme, but permits HTTP for local loopback development.
func isSecureProxyURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Scheme, "https") {
		return true
	}
	if strings.EqualFold(u.Scheme, "http") {
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
	}
	return false
}

// SafeCheckRedirect enforces redirect limits, restricts schemes to http/https,
// prevents HTTPS->HTTP downgrades, and strips Authorization headers across hosts.
func SafeCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
		return fmt.Errorf("unsupported redirect scheme: %s", req.URL.Scheme)
	}
	if len(via) > 0 {
		prev := via[len(via)-1]
		if prev.URL.Scheme == "https" && req.URL.Scheme == "http" {
			return fmt.Errorf("insecure redirect from HTTPS to HTTP forbidden")
		}
		if !strings.EqualFold(prev.URL.Hostname(), req.URL.Hostname()) ||
			prev.URL.Port() != req.URL.Port() {
			req.Header.Del("Authorization")
		}
	}
	return nil
}

// DefaultTransport returns a clone of http.DefaultTransport configured with
// production connection limits, timeouts, and modern TLS settings.
func DefaultTransport() http.RoundTripper {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := t.Clone()
		cloned.MaxIdleConns = config.HTTPMaxIdleConns
		cloned.MaxIdleConnsPerHost = config.CheckerConcurrencyLimit
		cloned.ResponseHeaderTimeout = config.HTTPResponseHeaderTimeout
		cloned.TLSHandshakeTimeout = config.HTTPTLSHandshakeTimeout
		cloned.IdleConnTimeout = config.HTTPIdleConnTimeout
		cloned.ExpectContinueTimeout = config.HTTPExpectContinueTimeout
		if cloned.TLSClientConfig == nil {
			cloned.TLSClientConfig = &tls.Config{}
		}
		cloned.TLSClientConfig.MinVersion = tls.VersionTLS12
		return cloned
	}
	return http.DefaultTransport
}

func defaultTransport() http.RoundTripper {
	return DefaultTransport()
}

// NewHTTPClient returns a hardened http.Client configured with
// standard timeouts, connection pooling, and redirect safety checks.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       config.HTTPTimeout,
		Transport:     DefaultTransport(),
		CheckRedirect: SafeCheckRedirect,
	}
}
