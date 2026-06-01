package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/chimanjain/gomajor/checker"
	"golang.org/x/mod/modfile"
)

// runChecker is the main execution entry point from rootCmd.Run.
func runChecker(fileExplicit, configExplicit, outputExplicit bool) {
	if err := runCheckerWithConfig(config, fileExplicit, configExplicit, outputExplicit); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// runCheckerWithConfig routes the analysis based on whether multi/remote configuration or single local run is selected.
func runCheckerWithConfig(cfg *Config, fileExplicit, configExplicit, outputExplicit bool) error {
	configPath := resolveConfigPath(cfg, configExplicit)
	if configPath != "" || len(cfg.GithubRepos) > 0 {
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
