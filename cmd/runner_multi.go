package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
)

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
				LatestMinorVersion: info.LatestMinorVersion,
				HasMinorUpdate:     info.HasMinorUpdate,
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

	// Merge command-line github repos into the configuration.
	if len(cfg.GithubRepos) > 0 {
		yamlCfg.Github = append(yamlCfg.Github, cfg.GithubRepos...)
	}

	if len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0 {
		return fmt.Errorf("no local or github sources specified")
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
