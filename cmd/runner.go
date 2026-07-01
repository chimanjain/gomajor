package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/chimanjain/gomajor/checker"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
)

// runChecker is the main execution entry point from rootCmd.Run.
func runChecker(ctx context.Context, fileExplicit, configExplicit, outputExplicit bool) {
	if err := runCheckerWithConfig(ctx, config, fileExplicit, configExplicit, outputExplicit); err != nil {
		_, _ = fmt.Fprintln(config.Err, "Error:", err)
		os.Exit(1)
	}
}

func runCheckerWithConfig(ctx context.Context, cfg *Config, fileExplicit, configExplicit, outputExplicit bool) error {
	origNoColor := color.NoColor
	defer func() {
		color.NoColor = origNoColor
	}()
	if cfg.NoColor {
		color.NoColor = true
	}

	cfg.GitHubRepos = normalizeGitHubRepos(cfg.GitHubRepos)

	configPath := resolveConfigPath(cfg, configExplicit)
	var results []SourceResult
	singleMode := configPath == "" && len(cfg.GitHubRepos) == 0

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

		if len(cfg.GitHubRepos) > 0 {
			yamlCfg.Github = append(yamlCfg.Github, cfg.GitHubRepos...)
		}

		httpClient := http.DefaultClient
		if cfg.Client != nil && cfg.Client.HTTPClient != nil {
			httpClient = cfg.Client.HTTPClient
		}

		var err error
		results, err = runMultiSources(ctx, cfg, yamlCfg, httpClient)
		if err != nil {
			return err
		}

		if cfg.OutputPath == "" {
			cfg.OutputPath = yamlCfg.Output
		}
	} else {
		var err error
		results, err = runLocalSource(ctx, cfg, fileExplicit, outputExplicit)
		if err != nil {
			return err
		}
	}

	outputPath := cfg.OutputPath
	isJSON := cfg.JSONOutput || strings.HasSuffix(strings.ToLower(outputPath), ".json")

	if outputExplicit && outputPath == "" {
		if isJSON {
			outputPath = "gomajor-report.json"
		} else {
			outputPath = "gomajor-report.yaml"
		}
	}

	if outputPath != "" {
		isJSON = cfg.JSONOutput || strings.HasSuffix(strings.ToLower(outputPath), ".json")
		return writeReport(outputPath, isJSON, results)
	}

	if cfg.JSONOutput {
		return printMultiJSONResults(cfg.Out, results)
	}

	printTextResults(cfg.Out, results, singleMode)
	return nil
}

func normalizeGitHubRepos(repos []string) []string {
	var normalized []string
	for _, repo := range repos {
		parts := strings.FieldsFunc(repo, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				normalized = append(normalized, part)
			}
		}
	}
	return normalized
}

func runLocalSource(ctx context.Context, cfg *Config, fileExplicit, outputExplicit bool) ([]SourceResult, error) {
	path := cfg.ModFilePath
	if !fileExplicit {
		resolved, err := resolveModFile()
		if err != nil {
			return nil, err
		}
		path = resolved
	}

	sourceRes, err := checkLocalMod(ctx, cfg, path)
	if err != nil {
		return nil, err
	}

	if !cfg.JSONOutput && !outputExplicit {
		printAnalysisHeader(cfg.Out, len(sourceRes.Dependencies), cfg.CheckAll, path)
	}
	return []SourceResult{sourceRes}, nil
}

type checkTask struct {
	index   int
	isLocal bool
	path    string
}

func runMultiSources(ctx context.Context, cfg *Config, yamlCfg YAMLConfig, httpClient *http.Client) ([]SourceResult, error) {
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

	// Deduplicate remote GitHub paths (handling potential space or comma separated lists)
	seenGithub := make(map[string]bool)
	var uniqueGithub []string
	for _, p := range yamlCfg.Github {
		parts := strings.FieldsFunc(p, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" && !seenGithub[part] {
				seenGithub[part] = true
				uniqueGithub = append(uniqueGithub, part)
			}
		}
	}
	yamlCfg.Github = uniqueGithub

	if len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0 {
		return nil, fmt.Errorf("no local or github sources specified")
	}

	var tasks []checkTask
	totalSources := len(yamlCfg.Local) + len(yamlCfg.Github)
	idx := 0
	for _, localPath := range yamlCfg.Local {
		tasks = append(tasks, checkTask{index: idx, isLocal: true, path: localPath})
		idx++
	}
	for _, githubPath := range yamlCfg.Github {
		tasks = append(tasks, checkTask{index: idx, isLocal: false, path: githubPath})
		idx++
	}

	resultsSlice := make([]SourceResult, totalSources)
	resultsValid := make([]bool, totalSources)
	var resultsMu sync.Mutex

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for _, task := range tasks {
		wg.Add(1)
		go func(t checkTask) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			var sourceRes SourceResult
			var err error

			if t.isLocal {
				sourceRes, err = checkLocalMod(ctx, cfg, t.path)
				if err != nil {
					_, _ = fmt.Fprintf(cfg.Err, "Warning: failed to check local go.mod at %s: %v\n", t.path, err)
					return
				}
			} else {
				sourceRes, err = checkGithubMod(ctx, cfg, httpClient, t.path)
				if err != nil {
					_, _ = fmt.Fprintf(cfg.Err, "Warning: failed to check github go.mod at %s: %v\n", sanitizeURL(t.path), err)
					return
				}
			}

			resultsMu.Lock()
			resultsSlice[t.index] = sourceRes
			resultsValid[t.index] = true
			resultsMu.Unlock()
		}(task)
	}

	wg.Wait()

	var results []SourceResult
	for i := 0; i < totalSources; i++ {
		if resultsValid[i] {
			results = append(results, resultsSlice[i])
		}
	}
	return results, nil
}

// resolveConfigPath determines the YAML configuration file path.
func resolveConfigPath(cfg *Config, configExplicit bool) string {
	configPath := cfg.ConfigPath
	if !configExplicit {
		if _, err := os.Stat("gomajor.yaml"); err == nil {
			return "gomajor.yaml"
		} else if !os.IsNotExist(err) {
			_, _ = fmt.Fprintf(cfg.Err, "Warning: failed to stat gomajor.yaml: %v\n", err)
		}
	}
	return configPath
}

// checkLocalMod reads and checks a local go.mod file.
func checkLocalMod(ctx context.Context, cfg *Config, path string) (SourceResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return SourceResult{}, fmt.Errorf("reading file: %w", err)
	}
	return checkModContent(ctx, cfg, path, "local", content)
}

// checkGithubMod fetches and checks a remote go.mod file from GitHub.
func checkGithubMod(ctx context.Context, cfg *Config, httpClient *http.Client, pathOrURL string) (SourceResult, error) {
	content, resolvedURL, err := fetchGithubMod(ctx, httpClient, pathOrURL)
	if err != nil {
		return SourceResult{}, err
	}
	return checkModContent(ctx, cfg, resolvedURL, "github", content)
}

// checkModContent parses the module content, checks dependencies, and generates a SourceResult.
func checkModContent(ctx context.Context, cfg *Config, sourceName string, sourceType string, content []byte) (SourceResult, error) {
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
		checked := checkDependencies(ctx, cfg, reqs)
		depInfos = toDependencyInfos(checked)
	}

	return SourceResult{
		Source:       sanitizeURL(sourceName),
		SourceType:   sourceType,
		Dependencies: depInfos,
	}, nil
}

// checkDependencies concurrently checks multiple module dependencies on the Go Module Proxy.
func checkDependencies(ctx context.Context, cfg *Config, reqs []*modfile.Require) []checker.ModuleInfo {
	if len(reqs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	resultsChan := make(chan checker.ModuleInfo, len(reqs))

	// Limit concurrent proxy queries to 20 to prevent socket/rate-limit issues
	sem := make(chan struct{}, 20)

	for _, req := range reqs {
		wg.Add(1)
		go func(modPath, version string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}
			info := cfg.Client.Check(ctx, modPath, version, cfg.MaxProbe)
			resultsChan <- info
		}(req.Mod.Path, req.Mod.Version)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	isTerminal := !cfg.JSONOutput && (isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd()))
	if isTerminal {
		_, _ = fmt.Fprintf(cfg.Err, "Checking dependencies: 0/%d completed...", len(reqs))
	}

	resultsMap := make(map[string]checker.ModuleInfo)
	completed := 0
	for info := range resultsChan {
		resultsMap[info.Current] = info
		if isTerminal {
			completed++
			_, _ = fmt.Fprintf(cfg.Err, "\rChecking dependencies: %d/%d completed...", completed, len(reqs))
		}
	}

	if isTerminal {
		_, _ = fmt.Fprint(cfg.Err, "\r\033[K") // Carriage return and clear line
	}

	var orderedResults []checker.ModuleInfo
	for _, req := range reqs {
		if info, exists := resultsMap[req.Mod.Path]; exists {
			orderedResults = append(orderedResults, info)
		}
	}

	return orderedResults
}

// sanitizeURL strips credentials (usernames, passwords, or tokens) from a raw URL.
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("redacted")
	return u.String()
}
