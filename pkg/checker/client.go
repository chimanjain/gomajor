package checker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/chimanjain/gomajor/internal/goenv"
	"github.com/chimanjain/gomajor/internal/retry"
	"github.com/chimanjain/gomajor/pkg/config"
	"golang.org/x/mod/module"
	"golang.org/x/sync/singleflight"
)

const (
	defaultProxy = "https://proxy.golang.org"
	directProxy  = "direct"
	offProxy     = "off"
)

var errNotFound = errors.New("module version not found")

// Client handles HTTP requests to the Go module proxy.
type Client struct {
	HTTPClient   *http.Client
	ProxyBase    string
	ProxyURLs    []string
	DisableMinor bool
	DisableMajor bool
	Logger       *slog.Logger
	BaseContext  context.Context

	latestCache    sync.Map // maps modPath to latest version found
	sfGroup        singleflight.Group
	sem            chan struct{}
	privateMatcher privateModuleMatcher
}

// Option configures a Client.
type Option func(*Client)

// NewClient creates a Client with default settings, then applies opts.
func NewClient(opts ...Option) *Client {
	c := &Client{
		HTTPClient: NewHTTPClient(),
		ProxyBase:  defaultProxy,
		ProxyURLs:  []string{defaultProxy},
		sem:        make(chan struct{}, config.CheckerConcurrencyLimit),
	}
	globs := cmp.Or(goenv.Get("GONOPROXY"), goenv.Get("GOPRIVATE"))
	c.privateMatcher.cache.Store(&globs)

	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DefaultClient returns a client configured from environment variables
// (GOPROXY) with production-grade HTTP transport settings.
// Any provided opts are applied after the environment-based configuration.
func DefaultClient(opts ...Option) *Client {
	c := NewClient()

	proxy := strings.ReplaceAll(cmp.Or(goenv.Get("GOPROXY"), defaultProxy), "|", ",")
	var proxyURLs []string
	var fallbackVal string
	for p := range strings.SplitSeq(proxy, ",") {
		p = strings.TrimRight(strings.TrimSpace(p), "/")
		if p == directProxy || p == offProxy {
			fallbackVal = p
		} else if p != "" && !slices.Contains(proxyURLs, p) {
			if isSecureProxyURL(p) {
				proxyURLs = append(proxyURLs, p)
			} else if fallbackVal == "" {
				fallbackVal = p
			}
		}
	}
	hasFallback := len(proxyURLs) == 0
	if hasFallback {
		proxyURLs = []string{defaultProxy}
	}

	WithProxyURLs(proxyURLs)(c)

	for _, opt := range opts {
		opt(c)
	}

	if hasFallback && fallbackVal != "" && c.Logger != nil {
		c.Logger.Warn("GOPROXY value not supported for major version discovery, falling back",
			"value", fallbackVal, "fallback", defaultProxy)
	}

	return c
}

// latestVersion returns the latest released version for a module path from the
// Go proxy. Returns ("", false) if nothing is found or an error occurs.
func (c *Client) latestVersion(ctx context.Context, modPath string) (string, bool) {
	if c.privateMatcher.isPrivate(modPath) {
		return "", false
	}

	if ver, ok := c.latestCache.Load(modPath); ok {
		return ver.(string), ver.(string) != ""
	}

	ch := c.sfGroup.DoChan(modPath, func() (any, error) {
		baseCtx := c.BaseContext
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		fetchCtx, cancel := context.WithTimeout(baseCtx, config.HTTPTimeout)
		defer cancel()
		version, ok := c.fetchLatestVersion(fetchCtx, modPath)
		if !ok {
			c.latestCache.Store(modPath, "")
			return "", fmt.Errorf("failed to fetch")
		}
		c.latestCache.Store(modPath, version)
		return version, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return "", false
		}
		return res.Val.(string), true
	case <-ctx.Done():
		return "", false
	}
}

// fetchLatestVersion performs the actual HTTP request to the Go proxy.
func (c *Client) fetchLatestVersion(ctx context.Context, modPath string) (string, bool) {
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-ctx.Done():
			return "", false
		}
	}

	escaped, err := module.EscapePath(modPath)
	if err != nil {
		return "", false
	}

	proxyList := c.ProxyURLs
	if len(proxyList) == 0 {
		proxyList = []string{cmp.Or(c.ProxyBase, defaultProxy)}
	}

	for _, proxyURL := range proxyList {
		ver, found, abort := c.fetchFromProxy(ctx, proxyURL, escaped)
		if found {
			return ver, true
		}
		if abort {
			return "", false
		}
	}

	return "", false
}

func (c *Client) fetchFromProxy(ctx context.Context, proxyURL, escaped string) (version string, found bool, abort bool) {
	url := fmt.Sprintf("%s/%s/@latest", proxyURL, escaped)

	baseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false, true
	}

	operation := func() (string, error) {
		req := baseReq.Clone(ctx)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return "", err
		}
		defer func() {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}()

		if resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(io.LimitReader(resp.Body, config.ProxyResponseMaxBytes))
			if err != nil {
				return "", retry.Permanent(fmt.Errorf("reading response: %w", err))
			}
			var info struct {
				Version string `json:"Version"`
			}
			if err := json.Unmarshal(data, &info); err != nil || info.Version == "" {
				return "", retry.Permanent(fmt.Errorf("invalid json: %w", err))
			}
			return info.Version, nil
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return "", retry.Permanent(errNotFound)
		}

		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable {
			return "", retry.Permanent(fmt.Errorf("status %d", resp.StatusCode))
		}
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	infoVersion, err := retry.Do(ctx, uint(config.HTTPMaxRetries), config.HTTPRetryInitialDelay, operation)
	if err == nil {
		return infoVersion, true, false
	}
	if errors.Is(err, errNotFound) {
		return "", false, false
	}
	if ctx.Err() != nil {
		return "", false, true
	}

	return "", false, false
}

// Close closes the idle HTTP connections of the client.
func (c *Client) Close() {
	if c.HTTPClient != nil && c.HTTPClient.Transport != nil {
		if tr, ok := c.HTTPClient.Transport.(interface{ CloseIdleConnections() }); ok {
			tr.CloseIdleConnections()
		}
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.HTTPClient = hc }
}

// WithProxyURLs sets the ordered list of Go module proxy URLs.
// Insecure non-loopback HTTP URLs are discarded to prevent MITM and SSRF.
func WithProxyURLs(urls []string) Option {
	return func(c *Client) {
		var secureURLs []string
		for _, u := range urls {
			u = strings.TrimRight(strings.TrimSpace(u), "/")
			if u != "" && isSecureProxyURL(u) {
				secureURLs = append(secureURLs, u)
			}
		}
		if len(secureURLs) == 0 && len(urls) > 0 {
			secureURLs = []string{defaultProxy}
		}
		c.ProxyURLs = secureURLs
		if len(secureURLs) > 0 {
			c.ProxyBase = secureURLs[0]
		}
	}
}

// WithDisableMinor disables minor version checking.
func WithDisableMinor(b bool) Option {
	return func(c *Client) { c.DisableMinor = b }
}

// WithDisableMajor disables major version checking.
func WithDisableMajor(b bool) Option {
	return func(c *Client) { c.DisableMajor = b }
}

// WithConcurrencyLimit sets the maximum number of concurrent HTTP requests.
func WithConcurrencyLimit(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.sem = make(chan struct{}, n)
		}
	}
}

// WithLogger sets the logger for the client.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) {
		c.Logger = logger
	}
}

// WithBaseContext sets the base context for background fetches, enabling graceful shutdown.
func WithBaseContext(ctx context.Context) Option {
	return func(c *Client) {
		c.BaseContext = ctx
	}
}
