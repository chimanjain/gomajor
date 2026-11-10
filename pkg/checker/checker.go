package checker

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/chimanjain/gomajor/internal/modpath"
	"github.com/chimanjain/gomajor/pkg/model"
	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"
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

// ModuleInfo holds information about a module and any discovered major update.
type ModuleInfo = model.DependencyInfo

type candResult struct {
	major int
	path  string
	ver   string
	ok    bool
}

// FindLatestMajor probes the Go proxy for higher major versions beyond currentMajor,
// up to a configurable ceiling. It returns the highest major version found and
// the module path for it. Candidate major versions are probed concurrently.
func (c *Client) FindLatestMajor(ctx context.Context, basePath string, currentMajor int, maxProbe int, sep string) (latestMajor int, latestPath string, latestVer string) {
	latestMajor = currentMajor
	latestPath = modpath.NextMajorPath(basePath, currentMajor, sep)
	if maxProbe <= 0 {
		return latestMajor, latestPath, latestVer
	}

	results := make([]candResult, maxProbe)
	var uncachedIndices []int
	for i := range maxProbe {
		cand := currentMajor + 1 + i
		candPath := modpath.NextMajorPath(basePath, cand, sep)
		if verVal, ok := c.latestCache.Load(candPath); ok {
			vStr, _ := verVal.(string)
			results[i] = candResult{major: cand, path: candPath, ver: vStr, ok: vStr != ""}
		} else {
			results[i] = candResult{major: cand, path: candPath}
			uncachedIndices = append(uncachedIndices, i)
		}
	}

	if len(uncachedIndices) == 1 {
		idx := uncachedIndices[0]
		ver, ok := c.latestVersion(ctx, results[idx].path)
		results[idx].ver = ver
		results[idx].ok = ok
	} else if len(uncachedIndices) > 1 {
		g, gCtx := errgroup.WithContext(ctx)
		for _, idx := range uncachedIndices {
			g.Go(func() error {
				if gCtx.Err() != nil {
					return gCtx.Err()
				}
				ver, ok := c.latestVersion(gCtx, results[idx].path)
				results[idx].ver = ver
				results[idx].ok = ok
				return nil
			})
		}
		_ = g.Wait()
	}

	for _, res := range results {
		if res.ok {
			latestMajor, latestPath, latestVer = res.major, res.path, res.ver
		}
	}
	return latestMajor, latestPath, latestVer
}

// Check analyses a single module (path + version from go.mod) and returns a ModuleInfo.
func (c *Client) Check(ctx context.Context, modPath, modVersion string, maxProbe int) ModuleInfo {
	basePath, currentMajor, sep := modpath.ParseModulePath(modPath)
	if semver.IsValid(modVersion) {
		if mv, err := strconv.Atoi(strings.TrimPrefix(semver.Major(modVersion), "v")); err == nil && mv > currentMajor {
			currentMajor = mv
		}
	}

	info := ModuleInfo{
		Module:          modPath,
		CurrentVersion:  modVersion,
		LatestMajorPath: modPath,
		CalculationMetadata: model.CalculationMetadata{
			Current:      modPath,
			BasePath:     basePath,
			CurrentMajor: currentMajor,
			LatestMajor:  currentMajor,
			Separator:    sep,
		},
	}

	if c.privateMatcher.isPrivate(modPath) || c.privateMatcher.isPrivate(basePath) {
		return info
	}

	checkMinor := func() {
		if lm, ok := c.latestVersion(ctx, modPath); ok && lm != "" && semver.Compare(lm, modVersion) > 0 {
			info.LatestMinorVersion, info.HasMinorUpdate = lm, true
		}
	}
	checkMajor := func() {
		info.LatestMajor, info.LatestMajorPath, info.LatestMajorVersion = c.FindLatestMajor(ctx, basePath, currentMajor, maxProbe, sep)
		info.HasUpdate = info.LatestMajor > currentMajor
	}

	var wg sync.WaitGroup
	if !c.DisableMinor {
		wg.Go(checkMinor)
	}
	if !c.DisableMajor {
		checkMajor()
	}
	wg.Wait()

	return info
}
