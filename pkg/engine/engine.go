package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/constants"
	"github.com/chimanjain/gomajor/pkg/model"
	"github.com/chimanjain/gomajor/pkg/source"
	"golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	Client           checker.ModChecker
	GitHubHTTPClient *http.Client
	MaxProbe         int
	CheckAll         bool
	Logger           *slog.Logger
	OnProgress       func(completed, total int)
}

type Engine struct {
	opts Options
}

type depKey struct {
	modPath string
	version string
}

// New creates an Engine using Options.
func New(opts Options) *Engine {
	if opts.Client == nil {
		opts.Client = checker.DefaultClient()
	}
	if opts.MaxProbe <= 0 {
		opts.MaxProbe = constants.DefaultMaxProbe
	}
	opts.MaxProbe = min(opts.MaxProbe, constants.MaxAllowedMaxProbe)
	return &Engine{opts: opts}
}

// NewWithOptions creates an Engine using explicit Options.
func NewWithOptions(opts Options) *Engine {
	return New(opts)
}

func (e *Engine) parseAllProviders(ctx context.Context, providers []source.Provider) ([]source.ParsedSource, error) {
	parsedSources := make([]source.ParsedSource, len(providers))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(constants.EngineConcurrencyLimit)

	for i, provider := range providers {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return gCtx.Err()
			}
			pSource, err := provider.Parse(gCtx, e.opts.GitHubHTTPClient)
			if err != nil {
				if e.opts.Logger != nil {
					e.opts.Logger.Warn("failed to parse source", "name", provider.Name(), "type", provider.Type(), "error", err)
				}
				return nil // Skip this source on error
			}
			parsedSources[i] = pSource
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	validSources := make([]source.ParsedSource, 0, len(parsedSources))
	for _, ps := range parsedSources {
		if ps.Source != "" {
			validSources = append(validSources, ps)
		}
	}
	return validSources, nil
}

func (e *Engine) CheckDependencies(ctx context.Context, reqs []*modfile.Require) ([]checker.ModuleInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	type uniqueCheck struct {
		modPath string
		version string
		indices []int
	}

	uniqueMap := make(map[depKey]*uniqueCheck)
	var uniques []*uniqueCheck

	for i, req := range reqs {
		k := depKey{modPath: req.Mod.Path, version: req.Mod.Version}
		uc, exists := uniqueMap[k]
		if !exists {
			uc = &uniqueCheck{
				modPath: req.Mod.Path,
				version: req.Mod.Version,
				indices: []int{i},
			}
			uniqueMap[k] = uc
			uniques = append(uniques, uc)
		} else {
			uc.indices = append(uc.indices, i)
		}
	}

	orderedResults := make([]checker.ModuleInfo, len(reqs))
	uniqueResults := make([]checker.ModuleInfo, len(uniques))

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(constants.EngineConcurrencyLimit)

	var completed atomic.Int32

	for i, uc := range uniques {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return gCtx.Err()
			}
			uniqueResults[i] = e.opts.Client.Check(gCtx, uc.modPath, uc.version, e.opts.MaxProbe)
			if e.opts.OnProgress != nil {
				// #nosec G115
				c := completed.Add(int32(len(uc.indices)))
				e.opts.OnProgress(int(c), len(reqs))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	for i, uc := range uniques {
		baseRes := uniqueResults[i]
		for _, idx := range uc.indices {
			orderedResults[idx] = baseRes
		}
	}

	return orderedResults, nil
}

func (e *Engine) checkParsedSources(ctx context.Context, validSources []source.ParsedSource) ([]model.SourceResult, error) {
	uniqueDeps := make(map[depKey]*modfile.Require)
	var depKeys []depKey

	for _, ps := range validSources {
		for _, req := range ps.Reqs {
			if !e.opts.CheckAll && req.Indirect {
				continue
			}
			k := depKey{modPath: req.Mod.Path, version: req.Mod.Version}
			if uniqueDeps[k] == nil {
				uniqueDeps[k] = req
				depKeys = append(depKeys, k)
			}
		}
	}

	globalReqs := make([]*modfile.Require, 0, len(depKeys))
	for _, k := range depKeys {
		globalReqs = append(globalReqs, uniqueDeps[k])
	}

	globalInfos, err := e.CheckDependencies(ctx, globalReqs)
	if err != nil {
		return nil, err
	}

	depResults := make(map[depKey]checker.ModuleInfo, len(globalInfos))
	for _, info := range globalInfos {
		depResults[depKey{modPath: info.Current, version: info.CurrentVersion}] = info
	}

	results := make([]model.SourceResult, 0, len(validSources))
	for _, ps := range validSources {
		depInfos := make([]model.DependencyInfo, 0, len(ps.Reqs))
		for _, req := range ps.Reqs {
			if !e.opts.CheckAll && req.Indirect {
				continue
			}
			k := depKey{modPath: req.Mod.Path, version: req.Mod.Version}
			if info, exists := depResults[k]; exists {
				depInfos = append(depInfos, model.DependencyInfo{
					Module:             info.Current,
					CurrentVersion:     info.CurrentVersion,
					LatestMajorVersion: info.LatestMajorVersion,
					LatestMajorPath:    info.LatestMajorPath,
					HasUpdate:          info.HasUpdate,
					LatestMinorVersion: info.LatestMinorVersion,
					HasMinorUpdate:     info.HasMinorUpdate,
					BasePath:           info.BasePath,
				})
			}
		}
		results = append(results, model.SourceResult{
			Source:       ps.Source,
			SourceType:   ps.SourceType,
			Dependencies: depInfos,
		})
	}

	return results, nil
}

func (e *Engine) RunMultiSources(ctx context.Context, yamlCfg config.YAMLConfig) ([]model.SourceResult, error) {
	normalizeSources(&yamlCfg)

	if len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0 {
		return nil, fmt.Errorf("no local or github sources specified")
	}

	providers := make([]source.Provider, 0, len(yamlCfg.Local)+len(yamlCfg.Github))
	for _, localPath := range yamlCfg.Local {
		providers = append(providers, source.NewLocalProvider(localPath))
	}
	for _, githubPath := range yamlCfg.Github {
		providers = append(providers, source.NewGitHubProvider(githubPath))
	}

	validSources, err := e.parseAllProviders(ctx, providers)
	if err != nil {
		return nil, err
	}

	return e.checkParsedSources(ctx, validSources)
}

func (e *Engine) RunLocalSource(ctx context.Context, path string) ([]model.SourceResult, error) {
	provider := source.NewLocalProvider(path)
	validSources, err := e.parseAllProviders(ctx, []source.Provider{provider})
	if err != nil {
		return nil, err
	}

	return e.checkParsedSources(ctx, validSources)
}

// deduplicateStrings returns in with duplicates removed, preserving order.
func deduplicateStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// normalizeSources deduplicates and normalises both local paths and GitHub repo
// strings in yamlCfg. GitHub entries support comma/space/tab separation, so
// each entry is split before deduplication.
// This is the single authoritative deduplication point for all source types;
// callers (e.g. cmd/runner.go) should pass raw values and rely on this function.
func normalizeSources(yamlCfg *config.YAMLConfig) {
	yamlCfg.Local = deduplicateStrings(yamlCfg.Local)

	var githubParts []string
	for _, p := range yamlCfg.Github {
		parts := strings.FieldsFunc(p, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		githubParts = append(githubParts, parts...)
	}
	yamlCfg.Github = deduplicateStrings(githubParts)
}
