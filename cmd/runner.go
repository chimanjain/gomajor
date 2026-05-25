package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/chimanjain/gomajor/checker"
	"github.com/fatih/color"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
)

func runChecker(fileExplicit, configExplicit, outputExplicit bool) {
	if err := runCheckerWithConfig(config, fileExplicit, configExplicit, outputExplicit); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func runCheckerWithConfig(cfg *Config, fileExplicit, configExplicit, outputExplicit bool) error {
	configPath := resolveConfigPath(cfg, configExplicit)

	if configPath != "" {
		return runMultiChecker(cfg, configPath, outputExplicit)
	}

	return runSingleChecker(cfg, fileExplicit)
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

	// Map results to preserve input requirement order
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

func runSingleChecker(cfg *Config, fileExplicit bool) error {
	// Resolve the path to go.mod.
	path := cfg.ModFilePath
	if !fileExplicit {
		resolved, err := resolveModFile()
		if err != nil {
			return err
		}
		path = resolved
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", path, err)
	}

	modFile, err := modfile.Parse(path, content, nil)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	var reqs []*modfile.Require
	for _, req := range modFile.Require {
		if !cfg.CheckAll && req.Indirect {
			continue
		}
		reqs = append(reqs, req)
	}

	if len(reqs) == 0 {
		if !cfg.JsonOutput {
			fmt.Println("No matching dependencies found in", path)
		} else {
			fmt.Println("[]")
		}
		return nil
	}

	if !cfg.JsonOutput {
		printAnalysisHeader(len(reqs), cfg.CheckAll, path)
	}

	allResults := checkDependencies(cfg, reqs)

	if cfg.JsonOutput {
		return printJsonResults(allResults)
	}

	hasUpdates := printTextResults(allResults)
	if !hasUpdates {
		fmt.Println(color.GreenString("✔ All checked dependencies are on their latest major versions."))
	}
	return nil
}

func checkLocalMod(cfg *Config, path string) (SourceResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return SourceResult{}, fmt.Errorf("reading file: %w", err)
	}

	return checkModContent(cfg, path, "local", content)
}

func checkGithubMod(cfg *Config, httpClient *http.Client, pathOrUrl string) (SourceResult, error) {
	ctx := context.Background()
	content, resolvedURL, err := fetchGithubMod(ctx, httpClient, pathOrUrl)
	if err != nil {
		return SourceResult{}, err
	}

	return checkModContent(cfg, resolvedURL, "github", content)
}

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
		for _, info := range checked {
			depInfos = append(depInfos, DependencyInfo{
				Module:             info.Current,
				CurrentVersion:     info.CurrentVersion,
				LatestMajorVersion: info.LatestMajorVersion,
				LatestMajorPath:    info.LatestMajorPath,
				HasUpdate:          info.HasUpdate,
			})
		}
	}

	return SourceResult{
		Source:       sourceName,
		SourceType:   sourceType,
		Dependencies: depInfos,
	}, nil
}

func runMultiChecker(cfg *Config, configPath string, outputExplicit bool) error {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config file %s: %w", configPath, err)
	}

	var yamlCfg YAMLConfig
	if err := yaml.Unmarshal(content, &yamlCfg); err != nil {
		return fmt.Errorf("parsing YAML config: %w", err)
	}

	if len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0 {
		return fmt.Errorf("no local or github sources specified in configuration file %s", configPath)
	}

	var results []SourceResult

	// 1. Process Local Paths
	for _, localPath := range yamlCfg.Local {
		sourceRes, err := checkLocalMod(cfg, localPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check local go.mod at %s: %v\n", localPath, err)
			continue
		}
		results = append(results, sourceRes)
	}

	// 2. Process GitHub Repositories
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

	// 3. Determine output destination & format
	outputPath := cfg.OutputPath
	if outputPath == "" {
		outputPath = yamlCfg.Output
	}

	if outputExplicit && outputPath == "" {
		outputPath = "gomajor-report.yaml"
	}

	if outputPath != "" {
		outputData := YAMLOutput{Results: results}
		yamlBytes, err := yaml.Marshal(outputData)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML output: %w", err)
		}
		if err := os.WriteFile(outputPath, yamlBytes, 0644); err != nil {
			return fmt.Errorf("failed to write YAML output to %s: %w", outputPath, err)
		}
		fmt.Printf("Results written to YAML file: %s\n", outputPath)
		return nil
	}

	printMultiTextResults(results)
	return nil
}
