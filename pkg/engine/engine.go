package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/model"
	"github.com/chimanjain/gomajor/pkg/source"
	"golang.org/x/sync/errgroup"
)

// DependencyChecker defines the interface required by Engine to inspect module dependencies.
// It is satisfied by *checker.Client and checker.ModChecker.
type DependencyChecker interface {
	Check(ctx context.Context, modPath, modVersion string, maxProbe int) model.DependencyInfo
	Close()
}

type Options struct {
	Client           DependencyChecker
	GitHubHTTPClient *http.Client
	MaxProbe         int
	CheckAll         bool
	Logger           *slog.Logger
	OnProgress       func(completed, total int)
}

type Engine struct {
	opts             Options
	concurrencyLimit int
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
	if opts.GitHubHTTPClient == nil {
		if cl, ok := opts.Client.(*checker.Client); ok && cl != nil && cl.HTTPClient != nil {
			opts.GitHubHTTPClient = cl.HTTPClient
		} else {
			opts.GitHubHTTPClient = checker.NewHTTPClient()
		}
	}
	if opts.MaxProbe <= 0 {
		opts.MaxProbe = config.DefaultMaxProbe
	}
	opts.MaxProbe = min(opts.MaxProbe, config.MaxAllowedMaxProbe)

	// Run workers up to EngineConcurrencyLimit. Active HTTP requests are
	// independently bounded by the checker client's semaphore (CheckerConcurrencyLimit),
	// preventing worker starvation and head-of-line blocking when individual requests stall.
	limit := config.EngineConcurrencyLimit

	return &Engine{
		opts:             opts,
		concurrencyLimit: limit,
	}
}

func (e *Engine) parseAllProviders(ctx context.Context, providers []source.Provider) ([]source.ParsedSource, error) {
	parsedSources := make([]source.ParsedSource, len(providers))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(config.EngineConcurrencyLimit)

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

func (e *Engine) checkUniqueDeps(ctx context.Context, uniqueKeys []depKey) (map[depKey]model.DependencyInfo, error) {
	if len(uniqueKeys) == 0 {
		return nil, nil
	}
	if len(uniqueKeys) == 1 {
		k := uniqueKeys[0]
		info := e.opts.Client.Check(ctx, k.modPath, k.version, e.opts.MaxProbe)
		if e.opts.OnProgress != nil {
			e.opts.OnProgress(1, 1)
		}
		return map[depKey]model.DependencyInfo{k: info}, nil
	}

	results := make([]model.DependencyInfo, len(uniqueKeys))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(e.concurrencyLimit)

	var completed atomic.Int32

	for i, k := range uniqueKeys {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return gCtx.Err()
			}
			results[i] = e.opts.Client.Check(gCtx, k.modPath, k.version, e.opts.MaxProbe)

			if e.opts.OnProgress != nil {
				// #nosec G115
				c := completed.Add(1)
				e.opts.OnProgress(int(c), len(uniqueKeys))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	uniqueDeps := make(map[depKey]model.DependencyInfo, len(uniqueKeys))
	for i, k := range uniqueKeys {
		uniqueDeps[k] = results[i]
	}
	return uniqueDeps, nil
}

func (e *Engine) checkParsedSources(ctx context.Context, validSources []source.ParsedSource) ([]model.SourceResult, error) {
	seen := make(map[depKey]bool)
	var uniqueKeys []depKey

	for _, ps := range validSources {
		for _, req := range ps.Reqs {
			if !e.opts.CheckAll && req.Indirect {
				continue
			}
			k := depKey{modPath: req.Mod.Path, version: req.Mod.Version}
			if !seen[k] {
				seen[k] = true
				uniqueKeys = append(uniqueKeys, k)
			}
		}
	}

	uniqueDeps, err := e.checkUniqueDeps(ctx, uniqueKeys)
	if err != nil {
		return nil, err
	}

	results := make([]model.SourceResult, 0, len(validSources))
	for _, ps := range validSources {
		depInfos := make([]model.DependencyInfo, 0, len(ps.Reqs))
		for _, req := range ps.Reqs {
			if !e.opts.CheckAll && req.Indirect {
				continue
			}
			k := depKey{modPath: req.Mod.Path, version: req.Mod.Version}
			if info, exists := uniqueDeps[k]; exists {
				depInfos = append(depInfos, info)
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

// Run processes all given providers, parses them, and executes dependency checking.
func (e *Engine) Run(ctx context.Context, providers []source.Provider) ([]model.SourceResult, error) {
	validSources, err := e.parseAllProviders(ctx, providers)
	if err != nil {
		return nil, err
	}
	return e.checkParsedSources(ctx, validSources)
}

// RunParsedSources executes dependency analysis on already parsed sources.
func (e *Engine) RunParsedSources(ctx context.Context, sources []source.ParsedSource) ([]model.SourceResult, error) {
	return e.checkParsedSources(ctx, sources)
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

	return e.Run(ctx, providers)
}

// deduplicateStrings returns in with duplicates removed, preserving order.
func deduplicateStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	return slices.DeleteFunc(in, func(s string) bool {
		if seen[s] {
			return true
		}
		seen[s] = true
		return false
	})
}

// normalizeSources deduplicates and normalises both local paths and GitHub repo
// strings in yamlCfg. GitHub entries support comma/space/tab separation, so
// each entry is split before deduplication.
func normalizeSources(yamlCfg *config.YAMLConfig) {
	yamlCfg.Local = deduplicateStrings(yamlCfg.Local)

	var githubParts []string
	for _, p := range yamlCfg.Github {
		parts := strings.FieldsFunc(p, func(r rune) bool {
			switch r {
			case ',', ' ', '\t', '\n', '\r':
				return true
			default:
				return false
			}
		})
		githubParts = append(githubParts, parts...)
	}
	yamlCfg.Github = deduplicateStrings(githubParts)
}
