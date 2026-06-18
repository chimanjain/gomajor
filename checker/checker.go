// Package checker contains the logic for detecting newer major versions
// of Go modules by querying the Go Module Proxy.
package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chimanjain/gomajor/utils"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/singleflight"
)

// Client handles HTTP requests to the Go module proxy.
type Client struct {
	HTTPClient *http.Client
	ProxyBase  string
	DisableMinor bool
	DisableMajor bool

	cacheMu     sync.RWMutex
	latestCache map[string]string // maps modPath to latest version found
	sfGroup     singleflight.Group
}

// DefaultClient returns a client with standard settings.
func DefaultClient() *Client {
	proxy := os.Getenv("GOPROXY")
	if proxy == "" {
		proxy = "https://proxy.golang.org"
	}
	// Take the first proxy in the list (e.g. "proxy1,proxy2,direct")
	if idx := strings.Index(proxy, ","); idx != -1 {
		proxy = proxy[:idx]
	}
	// If it's "direct" or "off", default to the standard proxy for this tool's purposes
	if proxy == "direct" || proxy == "off" {
		proxy = "https://proxy.golang.org"
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 20

	return &Client{
		HTTPClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		ProxyBase:   strings.TrimRight(proxy, "/"),
		latestCache: make(map[string]string),
	}
}

var defaultClient = DefaultClient()

// ProxyBase is deprecated; use Client.ProxyBase instead.
// Kept for backward compatibility with tests.
var ProxyBase = defaultClient.ProxyBase

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

// isPrivateModule checks if the module path matches GOPRIVATE or GONOPROXY environment variables.
func isPrivateModule(modPath string) bool {
	globs := os.Getenv("GONOPROXY")
	if globs == "" {
		globs = os.Getenv("GOPRIVATE")
	}
	if globs == "" {
		return false
	}
	return module.MatchPrefixPatterns(globs, modPath)
}

// latestVersion returns the latest released version for a module path from the
// Go proxy. Returns ("", false) if nothing is found or an error occurs.
func (c *Client) latestVersion(ctx context.Context, modPath string) (string, bool) {
	if isPrivateModule(modPath) {
		return "", false
	}

	c.cacheMu.RLock()
	if ver, ok := c.latestCache[modPath]; ok {
		c.cacheMu.RUnlock()
		return ver, ver != ""
	}
	c.cacheMu.RUnlock()

	val, err, _ := c.sfGroup.Do(modPath, func() (interface{}, error) {
		version, ok := c.fetchLatestVersion(ctx, modPath)
		if !ok {
			return "", fmt.Errorf("failed to fetch")
		}
		return version, nil
	})

	version := ""
	if err == nil {
		version = val.(string)
	}

	c.cacheMu.Lock()
	if c.latestCache == nil {
		c.latestCache = make(map[string]string)
	}
	c.latestCache[modPath] = version
	c.cacheMu.Unlock()

	return version, version != ""
}

// fetchLatestVersion performs the actual HTTP request to the Go proxy.
func (c *Client) fetchLatestVersion(ctx context.Context, modPath string) (string, bool) {
	escaped, err := utils.EscapePath(modPath)
	if err != nil {
		return "", false
	}
	url := fmt.Sprintf("%s/%s/@latest", c.ProxyBase, escaped)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", false
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(body, &info); err != nil || info.Version == "" {
		return "", false
	}
	return info.Version, true
}

// FindLatestMajor probes the Go proxy for higher major versions beyond currentMajor,
// up to a configurable ceiling. It returns the highest major version found and
// the module path for it.
func (c *Client) FindLatestMajor(ctx context.Context, basePath string, currentMajor int, maxProbe int, sep string) (latestMajor int, latestPath string, latestVer string) {
	latestMajor = currentMajor
	latestPath = utils.NextMajorPath(basePath, currentMajor, sep)
	if currentMajor == 1 && sep == "/" {
		latestPath = basePath
	}

	for candidate := currentMajor + 1; candidate <= currentMajor+maxProbe; candidate++ {
		candidatePath := utils.NextMajorPath(basePath, candidate, sep)
		ver, ok := c.latestVersion(ctx, candidatePath)
		if !ok {
			// Stop probing once we hit a gap.
			break
		}
		latestMajor = candidate
		latestPath = candidatePath
		latestVer = ver
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
	return defaultClient.Check(context.Background(), modPath, modVersion, maxProbe)
}
