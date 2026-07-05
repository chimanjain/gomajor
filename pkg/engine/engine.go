package engine

import (
	"context"
	"fmt"

	"github.com/chimanjain/gomajor/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/constants"
	"github.com/chimanjain/gomajor/pkg/source"
	"github.com/chimanjain/gomajor/utils"
	"golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"
)

type Engine struct {
	Config *config.Config
}

func New(cfg *config.Config) *Engine {
	return &Engine{
		Config: cfg,
	}
}

type checkTask struct {
	index   int
	isLocal bool
	path    string
}

func normalizeSources(yamlCfg *config.YAMLConfig) {
	seenLocal := make(map[string]bool)
	var uniqueLocal []string
	for _, p := range yamlCfg.Local {
		if !seenLocal[p] {
			seenLocal[p] = true
			uniqueLocal = append(uniqueLocal, p)
		}
	}
	yamlCfg.Local = uniqueLocal

	seenGithub := make(map[string]bool)
	var uniqueGithub []string
	for _, p := range yamlCfg.Github {
		parts := utils.NormalizeSplitString(p)
		for _, part := range parts {
			if !seenGithub[part] {
				seenGithub[part] = true
				uniqueGithub = append(uniqueGithub, part)
			}
		}
	}
	yamlCfg.Github = uniqueGithub
}

func (e *Engine) parseAllSources(ctx context.Context, tasks []checkTask) ([]source.ParsedSource, error) {
	parsedSources := make([]source.ParsedSource, len(tasks))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(constants.EngineConcurrencyLimit)

	for _, task := range tasks {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return gCtx.Err()
			}
			var pSource source.ParsedSource
			var err error

			if task.isLocal {
				pSource, err = source.ParseLocalMod(task.path)
				if err != nil {
					e.Config.Logger.Warn("failed to check local go.mod", "path", task.path, "error", err)
					return nil // Skip this source on error
				}
			} else {
				pSource, err = source.ParseGithubMod(gCtx, e.Config.Client.HTTPClient, task.path)
				if err != nil {
					e.Config.Logger.Warn("failed to check github go.mod", "path", source.SanitizeURL(task.path), "error", err)
					return nil
				}
			}
			parsedSources[task.index] = pSource
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var validSources []source.ParsedSource
	for _, ps := range parsedSources {
		if ps.Source != "" {
			validSources = append(validSources, ps)
		}
	}
	return validSources, nil
}

type depKey struct {
	modPath string
	version string
}

func (e *Engine) CheckDependencies(ctx context.Context, reqs []*modfile.Require) ([]checker.ModuleInfo, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	orderedResults := make([]checker.ModuleInfo, len(reqs))

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(constants.EngineConcurrencyLimit)

	for i, req := range reqs {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return gCtx.Err()
			}
			orderedResults[i] = e.Config.Client.Check(gCtx, req.Mod.Path, req.Mod.Version, e.Config.MaxProbe)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return orderedResults, nil
}

func (e *Engine) checkParsedSources(ctx context.Context, validSources []source.ParsedSource) ([]SourceResult, error) {
	uniqueDeps := make(map[depKey]*modfile.Require)
	var depKeys []depKey

	for _, ps := range validSources {
		for _, req := range ps.Reqs {
			if !e.Config.CheckAll && req.Indirect {
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

	depResults := make(map[depKey]checker.ModuleInfo)
	for _, info := range globalInfos {
		depResults[depKey{modPath: info.Current, version: info.CurrentVersion}] = info
	}

	var results []SourceResult
	for _, ps := range validSources {
		var depInfos []DependencyInfo
		for _, req := range ps.Reqs {
			if !e.Config.CheckAll && req.Indirect {
				continue
			}
			k := depKey{modPath: req.Mod.Path, version: req.Mod.Version}
			if info, exists := depResults[k]; exists {
				depInfos = append(depInfos, DependencyInfo{
					Module:             info.Current,
					CurrentVersion:     info.CurrentVersion,
					LatestMajorVersion: info.LatestMajorVersion,
					LatestMajorPath:    info.LatestMajorPath,
					HasUpdate:          info.HasUpdate,
					LatestMinorVersion: info.LatestMinorVersion,
					HasMinorUpdate:     info.HasMinorUpdate,
				})
			}
		}
		results = append(results, SourceResult{
			Source:       ps.Source,
			SourceType:   ps.SourceType,
			Dependencies: depInfos,
		})
	}

	return results, nil
}

func (e *Engine) RunMultiSources(ctx context.Context, yamlCfg config.YAMLConfig) ([]SourceResult, error) {
	normalizeSources(&yamlCfg)

	if len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0 {
		return nil, fmt.Errorf("no local or github sources specified")
	}

	var tasks []checkTask
	idx := 0
	for _, localPath := range yamlCfg.Local {
		tasks = append(tasks, checkTask{index: idx, isLocal: true, path: localPath})
		idx++
	}
	for _, githubPath := range yamlCfg.Github {
		tasks = append(tasks, checkTask{index: idx, isLocal: false, path: githubPath})
		idx++
	}

	validSources, err := e.parseAllSources(ctx, tasks)
	if err != nil {
		return nil, err
	}

	return e.checkParsedSources(ctx, validSources)
}

func (e *Engine) RunLocalSource(ctx context.Context, path string) ([]SourceResult, error) {
	parsed, err := source.ParseLocalMod(path)
	if err != nil {
		return nil, err
	}

	return e.checkParsedSources(ctx, []source.ParsedSource{parsed})
}
