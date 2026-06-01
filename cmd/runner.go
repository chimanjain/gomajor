package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/chimanjain/gomajor/checker"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
)

// runChecker is the main execution entry point from rootCmd.Run.
func runChecker(fileExplicit, configExplicit, outputExplicit bool) {
	if err := runCheckerWithConfig(config, fileExplicit, configExplicit, outputExplicit); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func runCheckerWithConfig(cfg *Config, fileExplicit, configExplicit, outputExplicit bool) error {
	configPath := resolveConfigPath(cfg, configExplicit)
	var results []SourceResult
	singleMode := configPath == "" && len(cfg.GithubRepos) == 0

	if !singleMode {
		var yamlCfg YAMLConfig
		if configPath != "" {
			content, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("reading config file %s: %w", configPath, err)
			}
			if err := yaml.Unmarshal(content, &yamlCfg); err != nil {
				return fmt.Errorf("parsing YAML config: %w", err)
			}
		}

		if yamlCfg.Minor != nil {
			cfg.Minor = *yamlCfg.Minor
			cfg.Client.DisableMinor = !cfg.Minor
		}
		if yamlCfg.Major != nil {
			cfg.Major = *yamlCfg.Major
			cfg.Client.DisableMajor = !cfg.Major
		}

		if len(cfg.GithubRepos) > 0 {
			yamlCfg.Github = append(yamlCfg.Github, cfg.GithubRepos...)
		}

		// Deduplicate local paths
		seenLocal := make(map[string]bool)
		var uniqueLocal []string
		for _, p := range yamlCfg.Local {
			if !seenLocal[p] {
				seenLocal[p] = true
				uniqueLocal = append(uniqueLocal, p)
			}
		}
		yamlCfg.Local = uniqueLocal

		// Deduplicate remote GitHub paths
		seenGithub := make(map[string]bool)
		var uniqueGithub []string
		for _, p := range yamlCfg.Github {
			if !seenGithub[p] {
				seenGithub[p] = true
				uniqueGithub = append(uniqueGithub, p)
			}
		}
		yamlCfg.Github = uniqueGithub

		if len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0 {
			return fmt.Errorf("no local or github sources specified")
		}

		for _, localPath := range yamlCfg.Local {
			sourceRes, err := checkLocalMod(cfg, localPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to check local go.mod at %s: %v\n", localPath, err)
				continue
			}
			results = append(results, sourceRes)
		}

		httpClient := http.DefaultClient
		if cfg.Client != nil && cfg.Client.HTTPClient != nil {
			httpClient = cfg.Client.HTTPClient
		}

		for _, githubPath := range yamlCfg.Github {
			sourceRes, err := checkGithubMod(cfg, httpClient, githubPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to check github go.mod at %s: %v\n", githubPath, err)
				continue
			}
			results = append(results, sourceRes)
		}

		if cfg.OutputPath == "" {
			cfg.OutputPath = yamlCfg.Output
		}
	} else {
		path := cfg.ModFilePath
		if !fileExplicit {
			resolved, err := resolveModFile()
			if err != nil {
				return err
			}
			path = resolved
		}

		sourceRes, err := checkLocalMod(cfg, path)
		if err != nil {
			return err
		}
		results = []SourceResult{sourceRes}

		if !cfg.JsonOutput && !outputExplicit {
			printAnalysisHeader(len(sourceRes.Dependencies), cfg.CheckAll, path)
		}
	}

	outputPath := cfg.OutputPath
	isJSON := cfg.JsonOutput || strings.HasSuffix(strings.ToLower(outputPath), ".json")

	if outputExplicit && outputPath == "" {
		if isJSON {
			outputPath = "gomajor-report.json"
		} else {
			outputPath = "gomajor-report.yaml"
		}
	}

	if outputPath != "" {
		isJSON = cfg.JsonOutput || strings.HasSuffix(strings.ToLower(outputPath), ".json")
		return writeReport(outputPath, isJSON, results)
	}

	if cfg.JsonOutput {
		return printMultiJsonResults(results)
	}

	printTextResults(results, singleMode)
	return nil
}

// resolveConfigPath determines the YAML configuration file path.
func resolveConfigPath(cfg *Config, configExplicit bool) string {
	configPath := cfg.ConfigPath
	if !configExplicit {
		if _, err := os.Stat("gomajor.yaml"); err == nil {
			return "gomajor.yaml"
		}
	}
	return configPath
}

// checkLocalMod reads and checks a local go.mod file.
func checkLocalMod(cfg *Config, path string) (SourceResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return SourceResult{}, fmt.Errorf("reading file: %w", err)
	}
	return checkModContent(cfg, path, "local", content)
}

// checkGithubMod fetches and checks a remote go.mod file from GitHub.
func checkGithubMod(cfg *Config, httpClient *http.Client, pathOrUrl string) (SourceResult, error) {
	ctx := context.Background()
	content, resolvedURL, err := fetchGithubMod(ctx, httpClient, pathOrUrl)
	if err != nil {
		return SourceResult{}, err
	}
	return checkModContent(cfg, resolvedURL, "github", content)
}

// checkModContent parses the module content, checks dependencies, and generates a SourceResult.
func checkModContent(cfg *Config, sourceName string, sourceType string, content []byte) (SourceResult, error) {
	modFile, err := modfile.Parse(sourceName, content, nil)
	if err != nil {
		return SourceResult{}, fmt.Errorf("parsing go.mod: %w", err)
	}

	var reqs []*modfile.Require
	for _, req := range modFile.Require {
		if !cfg.CheckAll && req.Indirect {
			continue
		}
		reqs = append(reqs, req)
	}

	var depInfos []DependencyInfo
	if len(reqs) > 0 {
		checked := checkDependencies(cfg, reqs)
		depInfos = toDependencyInfos(checked)
	}

	return SourceResult{
		Source:       sourceName,
		SourceType:   sourceType,
		Dependencies: depInfos,
	}, nil
}

// checkDependencies concurrently checks multiple module dependencies on the Go Module Proxy.
func checkDependencies(cfg *Config, reqs []*modfile.Require) []checker.ModuleInfo {
	if len(reqs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	resultsChan := make(chan checker.ModuleInfo, len(reqs))

	for _, req := range reqs {
		wg.Add(1)
		go func(modPath, version string) {
			defer wg.Done()
			info := cfg.Client.Check(context.Background(), modPath, version, cfg.MaxProbe)
			resultsChan <- info
		}(req.Mod.Path, req.Mod.Version)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	resultsMap := make(map[string]checker.ModuleInfo)
	for info := range resultsChan {
		resultsMap[info.Current] = info
	}

	var orderedResults []checker.ModuleInfo
	for _, req := range reqs {
		if info, exists := resultsMap[req.Mod.Path]; exists {
			orderedResults = append(orderedResults, info)
		}
	}

	return orderedResults
}
