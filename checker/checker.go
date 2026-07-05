package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/cenkalti/backoff/v7"
	"github.com/chimanjain/gomajor/pkg/constants"
	"github.com/chimanjain/gomajor/utils"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/singleflight"
)

const (
	defaultProxy = "https://proxy.golang.org"
	directProxy  = "direct"
	offProxy     = "off"
)

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

// DefaultClient returns a client with standard settings.
func DefaultClient() *Client {
	proxy := os.Getenv("GOPROXY")
	if proxy == "" {
		proxy = defaultProxy
	}

	var proxyURLs []string
	proxy = strings.ReplaceAll(proxy, "|", ",")
	parts := strings.SplitSeq(proxy, ",")
	for p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			p = strings.TrimRight(p, "/")
			if p == directProxy || p == offProxy {
				slog.Warn("GOPROXY value not supported for major version discovery, falling back",
					"value", p, "fallback", defaultProxy)
				proxyURLs = append(proxyURLs, defaultProxy)
			} else {
				proxyURLs = append(proxyURLs, p)
			}
		}
	}
	if len(proxyURLs) == 0 {
		proxyURLs = []string{defaultProxy}
	}

	transport := http.DefaultTransport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := t.Clone()
		cloned.MaxIdleConns = constants.HTTPMaxIdleConns
		cloned.MaxIdleConnsPerHost = constants.CheckerConcurrencyLimit
		transport = cloned
	}

	c := &Client{
		HTTPClient: &http.Client{
			Timeout:   constants.HTTPTimeout,
			Transport: transport,
		},
		ProxyBase: proxyURLs[0],
		ProxyURLs: proxyURLs,
		sem:       make(chan struct{}, constants.CheckerConcurrencyLimit),
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

var privateGlobs = func() string {
	globs := os.Getenv("GONOPROXY")
	if globs == "" {
		globs = os.Getenv("GOPRIVATE")
	}
	return globs
}()

// isPrivateModule checks if the module path matches GOPRIVATE or GONOPROXY environment variables.
func isPrivateModule(modPath string) bool {
	if privateGlobs == "" {
		return false
	}
	return module.MatchPrefixPatterns(privateGlobs, modPath)
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

	escaped, err := utils.EscapePath(modPath)
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
			if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.Version == "" {
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
	latestPath = utils.NextMajorPath(basePath, currentMajor, sep)

	remainingStart := currentMajor + 1
	remainingEnd := currentMajor + maxProbe
	if remainingStart > remainingEnd {
		return latestMajor, latestPath, latestVer
	}

	for cand := remainingStart; cand <= remainingEnd; cand++ {
		if ctx.Err() != nil {
			break
		}
		candPath := utils.NextMajorPath(basePath, cand, sep)
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
	basePath, currentMajor, sep := utils.ParseModulePath(modPath)
	info := ModuleInfo{
		Current:        modPath,
		CurrentVersion: modVersion,
		BasePath:       basePath,
		CurrentMajor:   currentMajor,
		Separator:      sep,
	}

	if !c.DisableMajor {
		latestMajor, latestPath, latestVer := c.FindLatestMajor(ctx, basePath, currentMajor, maxProbe, sep)
		info.LatestMajor = latestMajor
		info.LatestMajorPath = latestPath
		info.LatestMajorVersion = latestVer
		info.HasUpdate = latestMajor > currentMajor
	} else {
		info.LatestMajor = currentMajor
		info.LatestMajorPath = modPath
	}

	if !c.DisableMinor {
		latestMinor, ok := c.latestVersion(ctx, modPath)
		if ok && latestMinor != "" && semver.Compare(latestMinor, modVersion) > 0 {
			info.LatestMinorVersion = latestMinor
			info.HasMinorUpdate = true
		}
	}

	return info
}

// Check is a convenience function that uses the default client.
func Check(modPath, modVersion string, maxProbe int) ModuleInfo {
	return getDefaultClient().Check(context.Background(), modPath, modVersion, maxProbe)
}

// Close closes the idle HTTP connections of the client.
func (c *Client) Close() {
	if c.HTTPClient != nil && c.HTTPClient.Transport != nil {
		if tr, ok := c.HTTPClient.Transport.(interface{ CloseIdleConnections() }); ok {
			tr.CloseIdleConnections()
		}
	}
}
