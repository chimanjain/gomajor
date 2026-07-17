package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/cenkalti/backoff/v7"
	"github.com/chimanjain/gomajor/internal/modpath"
	"github.com/chimanjain/gomajor/pkg/constants"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/singleflight"
)

const (
	defaultProxy = "https://proxy.golang.org"
	directProxy  = "direct"
	offProxy     = "off"
)

// ModChecker is the interface satisfied by *Client and accepted by config.Config.
// It allows callers to substitute a test double without constructing a real HTTP
// client.
type ModChecker interface {
	// Check analyses a single module and returns update information.
	Check(ctx context.Context, modPath, modVersion string, maxProbe int) ModuleInfo
	// Close releases any idle HTTP connections held by the implementation.
	Close()
}

// Client handles HTTP requests to the Go module proxy.
type Client struct {
	HTTPClient   *http.Client
	ProxyBase    string
	ProxyURLs    []string
	DisableMinor bool
	DisableMajor bool

	initOnce    sync.Once
	latestCache sync.Map // maps modPath to latest version found
	sfGroup     singleflight.Group
	sem         chan struct{}
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.HTTPClient = hc }
}

// WithProxyURLs sets the ordered list of Go module proxy URLs.
func WithProxyURLs(urls []string) Option {
	return func(c *Client) {
		c.ProxyURLs = urls
		if len(urls) > 0 {
			c.ProxyBase = urls[0]
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

// NewClient creates a Client with default settings, then applies opts.
func NewClient(opts ...Option) *Client {
	c := &Client{
		HTTPClient: &http.Client{
			Timeout: constants.HTTPTimeout,
		},
		ProxyBase: defaultProxy,
		ProxyURLs: []string{defaultProxy},
		sem:       make(chan struct{}, constants.CheckerConcurrencyLimit),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DefaultClient returns a client configured from environment variables
// (GOPROXY) with production-grade HTTP transport settings.
// Any provided opts are applied after the environment-based configuration.
func DefaultClient(opts ...Option) *Client {
	proxy := os.Getenv("GOPROXY")
	if proxy == "" {
		proxy = defaultProxy
	}

	var proxyURLs []string
	proxy = strings.ReplaceAll(proxy, "|", ",")
	parts := strings.SplitSeq(proxy, ",")
	var containsFallback bool
	var fallbackVal string
	for p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			p = strings.TrimRight(p, "/")
			if p == directProxy || p == offProxy {
				containsFallback = true
				fallbackVal = p
			} else {
				alreadyPresent := slices.Contains(proxyURLs, p)
				if !alreadyPresent {
					proxyURLs = append(proxyURLs, p)
				}
			}
		}
	}
	if len(proxyURLs) == 0 {
		if containsFallback {
			slog.Warn("GOPROXY value not supported for major version discovery, falling back",
				"value", fallbackVal, "fallback", defaultProxy)
		}
		proxyURLs = []string{defaultProxy}
	}

	transport := http.DefaultTransport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := t.Clone()
		cloned.MaxIdleConns = constants.HTTPMaxIdleConns
		cloned.MaxIdleConnsPerHost = constants.CheckerConcurrencyLimit
		transport = cloned
	}

	c := NewClient(
		WithHTTPClient(&http.Client{
			Timeout:   constants.HTTPTimeout,
			Transport: transport,
		}),
		WithProxyURLs(proxyURLs),
	)
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) lazyInit() {
	c.initOnce.Do(func() {
		if c.sem == nil {
			c.sem = make(chan struct{}, constants.CheckerConcurrencyLimit)
		}
	})
}

var (
	defaultClient     *Client
	defaultClientOnce sync.Once
)

func getDefaultClient() *Client {
	defaultClientOnce.Do(func() {
		defaultClient = DefaultClient()
	})
	return defaultClient
}

// ModuleInfo holds information about a module and any discovered major update.
type ModuleInfo struct {
	// Current is the module path as it appears in go.mod (e.g. github.com/user/gomodule/v2).
	Current string
	// CurrentVersion is the semver version currently required (e.g. v2.50.0).
	CurrentVersion string
	// BasePath is the module path without the major-version suffix (e.g. github.com/user/gomodule).
	BasePath string
	// Separator is the version suffix separator used in this module path ("/" or ".").
	Separator string
	// CurrentMajor is the currently used major version number (1 for unversioned, 2+ otherwise).
	CurrentMajor int
	// LatestMajor is the highest major version found on the proxy.
	LatestMajor int
	// LatestMajorPath is the module path for the latest major version.
	LatestMajorPath string
	// LatestMajorVersion is the latest semver tag found for the newest major.
	LatestMajorVersion string
	// HasUpdate is true when LatestMajor > CurrentMajor.
	HasUpdate bool
	// LatestMinorVersion is the latest semver tag found for the current major version.
	LatestMinorVersion string
	// HasMinorUpdate is true when LatestMinorVersion is semantically greater than CurrentVersion.
	HasMinorUpdate bool
}

// getPrivateGlobs reads GONOPROXY / GOPRIVATE lazily on each call so that
// changes made after package init (e.g., t.Setenv in tests) are respected.
func getPrivateGlobs() string {
	globs := os.Getenv("GONOPROXY")
	if globs == "" {
		globs = os.Getenv("GOPRIVATE")
	}
	return globs
}

// isPrivateModule checks if the module path matches GOPRIVATE or GONOPROXY environment variables.
func isPrivateModule(modPath string) bool {
	globs := getPrivateGlobs()
	if globs == "" {
		return false
	}
	return module.MatchPrefixPatterns(globs, modPath)
}

// latestVersion returns the latest released version for a module path from the
// Go proxy. Returns ("", false) if nothing is found or an error occurs.
//
// Concurrent callers for the same modPath are coalesced by singleflight.
// The result is cached in latestCache once the fetch completes, so subsequent
// callers pay no network cost. A caller whose ctx is cancelled returns early
// without aborting the shared in-flight fetch, which is bounded by HTTPTimeout
// and may still benefit other waiting goroutines.
func (c *Client) latestVersion(ctx context.Context, modPath string) (string, bool) {
	if isPrivateModule(modPath) {
		return "", false
	}

	c.lazyInit()

	if ver, ok := c.latestCache.Load(modPath); ok {
		return ver.(string), ver.(string) != ""
	}

	ch := c.sfGroup.DoChan(modPath, func() (interface{}, error) {
		// Use an independent context bounded by HTTPTimeout so that one
		// caller cancelling does not abort a shared fetch that other
		// goroutines still depend on.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.HTTPTimeout)
		defer cancel()
		version, ok := c.fetchLatestVersion(fetchCtx, modPath)
		// Always populate the cache inside singleflight so the result is
		// stored regardless of whether any callers are still waiting.
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
	sem := c.sem
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return "", false
	}
	defer func() { <-sem }()

	escaped, err := module.EscapePath(modPath)
	if err != nil {
		return "", false
	}

	var proxyList []string
	switch {
	case len(c.ProxyURLs) > 0:
		proxyList = c.ProxyURLs
	case c.ProxyBase != "":
		proxyList = []string{c.ProxyBase}
	default:
		proxyList = []string{defaultProxy}
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

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = constants.HTTPRetryInitialDelay

	maxTries := uint(constants.HTTPMaxRetries)
	if maxTries == 0 {
		maxTries = 1
	}

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
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var info struct {
				Version string `json:"Version"`
			}
			limitReader := io.LimitReader(resp.Body, constants.ProxyResponseMaxBytes)
			if err := json.NewDecoder(limitReader).Decode(&info); err != nil || info.Version == "" {
				return "", backoff.Permanent(fmt.Errorf("invalid json: %w", err))
			}
			return info.Version, nil
		}

		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if !retryable {
			return "", backoff.Permanent(fmt.Errorf("status %d", resp.StatusCode))
		}
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	infoVersion, err := backoff.Retry(ctx, operation, backoff.WithBackOff(b), backoff.WithMaxTries(maxTries))

	if err == nil {
		return infoVersion, true, false
	}

	if ctx.Err() != nil {
		return "", false, true
	}

	return "", false, false
}

// FindLatestMajor probes the Go proxy for higher major versions beyond currentMajor,
// up to a configurable ceiling. It returns the highest major version found and
// the module path for it.
func (c *Client) FindLatestMajor(ctx context.Context, basePath string, currentMajor int, maxProbe int, sep string) (latestMajor int, latestPath string, latestVer string) {
	latestMajor = currentMajor
	latestPath = modpath.NextMajorPath(basePath, currentMajor, sep)

	remainingStart := currentMajor + 1
	remainingEnd := currentMajor + maxProbe
	if remainingStart > remainingEnd {
		return latestMajor, latestPath, latestVer
	}

	for cand := remainingStart; cand <= remainingEnd; cand++ {
		if ctx.Err() != nil {
			break
		}
		candPath := modpath.NextMajorPath(basePath, cand, sep)
		ver, ok := c.latestVersion(ctx, candPath)
		if ok {
			latestMajor = cand
			latestPath = candPath
			latestVer = ver
		} else {
			// If a major version is missing, assume subsequent ones are also missing
			// to avoid spamming the proxy with unnecessary requests.
			break
		}
	}

	return latestMajor, latestPath, latestVer
}

// Check analyses a single module (path + version from go.mod) and returns a ModuleInfo.
func (c *Client) Check(ctx context.Context, modPath, modVersion string, maxProbe int) ModuleInfo {
	basePath, currentMajor, sep := modpath.ParseModulePath(modPath)
	if semver.IsValid(modVersion) {
		majorStr := semver.Major(modVersion)
		if len(majorStr) > 1 && majorStr[0] == 'v' {
			if mv, err := strconv.Atoi(majorStr[1:]); err == nil {
				if mv > currentMajor {
					currentMajor = mv
				}
			}
		}
	}

	info := ModuleInfo{
		Current:        modPath,
		CurrentVersion: modVersion,
		BasePath:       basePath,
		CurrentMajor:   currentMajor,
		Separator:      sep,
	}

	// Use local variables per goroutine to avoid data races on the shared
	// info struct. Results are merged into info only after wg.Wait().
	var wg sync.WaitGroup

	var (
		latestMajor     int
		latestMajorPath string
		latestMajorVer  string
		hasMajorUpdate  bool
	)

	if !c.DisableMajor {
		wg.Go(func() {
			lm, lp, lv := c.FindLatestMajor(ctx, basePath, currentMajor, maxProbe, sep)
			latestMajor = lm
			latestMajorPath = lp
			latestMajorVer = lv
			hasMajorUpdate = lm > currentMajor
		})
	} else {
		latestMajor = currentMajor
		latestMajorPath = modPath
	}

	var latestMinor string
	var hasMinorUpdate bool
	if !c.DisableMinor {
		wg.Go(func() {
			lm, ok := c.latestVersion(ctx, modPath)
			if ok && lm != "" && semver.Compare(lm, modVersion) > 0 {
				latestMinor = lm
				hasMinorUpdate = true
			}
		})
	}

	wg.Wait()

	// Merge goroutine results into info after all goroutines have finished.
	if !c.DisableMajor {
		info.LatestMajor = latestMajor
		info.LatestMajorPath = latestMajorPath
		info.LatestMajorVersion = latestMajorVer
		info.HasUpdate = hasMajorUpdate
	} else {
		info.LatestMajor = currentMajor
		info.LatestMajorPath = modPath
	}
	info.LatestMinorVersion = latestMinor
	info.HasMinorUpdate = hasMinorUpdate

	return info
}

// Check is a convenience function that uses the default client.
func Check(ctx context.Context, modPath, modVersion string, maxProbe int) ModuleInfo {
	return getDefaultClient().Check(ctx, modPath, modVersion, maxProbe)
}

// Close closes the idle HTTP connections of the client.
func (c *Client) Close() {
	if c.HTTPClient != nil && c.HTTPClient.Transport != nil {
		if tr, ok := c.HTTPClient.Transport.(interface{ CloseIdleConnections() }); ok {
			tr.CloseIdleConnections()
		}
	}
}
