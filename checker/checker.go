// Package checker contains the logic for detecting newer major versions
// of Go modules by querying the Go Module Proxy.
package checker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ProxyBase = "https://proxy.golang.org"

// majorSuffixRe matches a trailing major-version segment in a module path.
// Handles both "/vN" (GitHub style) and ".vN" (gopkg.in style).
var majorSuffixRe = regexp.MustCompile(`((?:/|\.)(v(?:[2-9]|[1-9]\d+)))$`)

// ModuleInfo holds information about a module and any discovered major update.
type ModuleInfo struct {
	// Current is the module path as it appears in go.mod (e.g. github.com/user/gomodule/v2).
	Current string
	// CurrentVersion is the semver version currently required (e.g. v2.50.0).
	CurrentVersion string
	// BasePath is the module path without the major-version suffix (e.g. github.com/user/gomodule).
	BasePath string
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
}

// httpClient is a shared client with a reasonable timeout.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// ParseModulePath splits a module path into its base path and current major version number.
//
// Examples:
//
//	"github.com/user/gomodule/v2"  -> ("github.com/user/gomodule", 2)
//	"gopkg.in/yaml.v2"             -> ("gopkg.in/yaml", 2)
//	"github.com/google/uuid"       -> ("github.com/google/uuid", 1)
func ParseModulePath(modPath string) (basePath string, major int) {
	loc := majorSuffixRe.FindStringSubmatchIndex(modPath)
	if loc == nil {
		return modPath, 1
	}
	// loc[2]:loc[3] is the full separator+vN match; loc[4]:loc[5] is just "vN"
	vStr := modPath[loc[4]:loc[5]] // e.g. "v2"
	n, err := strconv.Atoi(strings.TrimPrefix(vStr, "v"))
	if err != nil || n < 2 {
		return modPath, 1
	}
	// Remove the matched suffix from the path.
	base := modPath[:loc[2]]
	return base, n
}

// nextMajorPath builds the module path for the given major version, respecting
// the gopkg.in ".vN" convention vs the standard "/vN" convention.
func nextMajorPath(basePath string, major int) string {
	// gopkg.in uses ".vN" convention.
	if strings.HasPrefix(basePath, "gopkg.in/") {
		return fmt.Sprintf("%s.v%d", basePath, major)
	}
	return fmt.Sprintf("%s/v%d", basePath, major)
}

// latestVersion returns the latest released version for a module path from the
// Go proxy. Returns ("", false) if nothing is found or an error occurs.
func latestVersion(modPath string) (string, bool) {
	// Use the /@latest endpoint which returns the latest tagged version.
	escaped, err := escapePath(modPath)
	if err != nil {
		return "", false
	}
	url := fmt.Sprintf("%s/%s/@latest", ProxyBase, escaped)
	resp, err := httpClient.Get(url)
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

// escapePath applies Go module path escaping (uppercase letters become !lowercase).
func escapePath(modPath string) (string, error) {
	var sb strings.Builder
	for _, r := range modPath {
		if r >= 'A' && r <= 'Z' {
			sb.WriteByte('!')
			sb.WriteRune(r + 32)
		} else {
			sb.WriteRune(r)
		}
	}
	// Validate the path is not empty.
	if sb.Len() == 0 {
		return "", fmt.Errorf("empty module path")
	}
	_ = path.Clean // just to ensure path is imported if needed later
	return sb.String(), nil
}

// FindLatestMajor probes the Go proxy for higher major versions beyond currentMajor,
// up to a configurable ceiling. It returns the highest major version found and
// the module path for it.
func FindLatestMajor(basePath string, currentMajor int, maxProbe int) (latestMajor int, latestPath string, latestVer string) {
	latestMajor = currentMajor
	latestPath = nextMajorPath(basePath, currentMajor)
	if currentMajor == 1 {
		latestPath = basePath
	}

	for candidate := currentMajor + 1; candidate <= currentMajor+maxProbe; candidate++ {
		candidatePath := nextMajorPath(basePath, candidate)
		ver, ok := latestVersion(candidatePath)
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
func Check(modPath, modVersion string, maxProbe int) ModuleInfo {
	basePath, currentMajor := ParseModulePath(modPath)
	info := ModuleInfo{
		Current:        modPath,
		CurrentVersion: modVersion,
		BasePath:       basePath,
		CurrentMajor:   currentMajor,
	}

	latestMajor, latestPath, latestVer := FindLatestMajor(basePath, currentMajor, maxProbe)
	info.LatestMajor = latestMajor
	info.LatestMajorPath = latestPath
	info.LatestMajorVersion = latestVer
	info.HasUpdate = latestMajor > currentMajor
	return info
}
