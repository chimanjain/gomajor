package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/chimanjain/gomajor/utils"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/spf13/viper"
)

const jsonExt = ".json"

func runCheckerWithConfig(ctx context.Context, cfg *config.Config, fileExplicit, configExplicit, outputExplicit bool) error {
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	if cfg.Err == nil {
		cfg.Err = io.Discard
	}
	if cfg.Client != nil {
		defer cfg.Client.Close()
	}

	origNoColor := color.NoColor
	defer func() {
		color.NoColor = origNoColor
	}()
	if cfg.NoColor {
		color.NoColor = true
	}

	cfg.GitHubRepos = normalizeGitHubRepos(cfg.GitHubRepos)
	configPath := resolveConfigPath(cfg, configExplicit)
	singleMode := configPath == "" && len(cfg.GitHubRepos) == 0

	if cfg.Err == os.Stderr && isatty.IsTerminal(os.Stderr.Fd()) && !cfg.JSONOutput {
		cfg.OnProgress = func(completed, total int) {
			fmt.Fprintf(cfg.Err, "\r%s [%d/%d]", color.HiBlackString("Checking dependencies..."), completed, total)
			if completed == total {
				fmt.Fprintln(cfg.Err)
			}
		}
	}

	eng := engine.New(cfg)
	var results []engine.SourceResult
	var err error

	if !singleMode {
		yamlCfg, err := loadViperConfig(cfg, configPath, configExplicit)
		if err != nil {
			return err
		}
		results, err = eng.RunMultiSources(ctx, yamlCfg)
		if err != nil {
			return err
		}
		if cfg.OutputPath == "" {
			cfg.OutputPath = yamlCfg.Output
		}
	} else {
		results, err = executeSingleSource(ctx, eng, cfg, fileExplicit, outputExplicit)
		if err != nil {
			return err
		}
	}

	return writeOutput(cfg, results, outputExplicit, singleMode)
}

func loadViperConfig(cfg *config.Config, configPath string, configExplicit bool) (config.YAMLConfig, error) {
	var yamlCfg config.YAMLConfig
	v := viper.New()
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			var notFoundErr viper.ConfigFileNotFoundError
			isNotFound := errors.As(err, &notFoundErr) || os.IsNotExist(err)

			if configExplicit || !isNotFound {
				return yamlCfg, fmt.Errorf("reading config file %s: %w", configPath, err)
			}
		} else {
			if err := v.Unmarshal(&yamlCfg); err != nil {
				return yamlCfg, fmt.Errorf("parsing config: %w", err)
			}
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

	return yamlCfg, nil
}

func executeSingleSource(ctx context.Context, eng *engine.Engine, cfg *config.Config, fileExplicit, outputExplicit bool) ([]engine.SourceResult, error) {
	path := cfg.ModFilePath
	if !fileExplicit {
		resolved, err := resolveModFile()
		if err != nil {
			return nil, err
		}
		path = resolved
	}

	results, err := eng.RunLocalSource(ctx, path)
	if err != nil {
		return nil, err
	}

	if !cfg.JSONOutput && !outputExplicit && len(results) > 0 {
		count := len(results[0].Dependencies)
		format.PrintAnalysisHeader(cfg.Out, count, cfg.CheckAll, path)
	}
	return results, nil
}

func writeOutput(cfg *config.Config, results []engine.SourceResult, outputExplicit, singleMode bool) error {
	outputPath := cfg.OutputPath
	isJSON := cfg.JSONOutput || strings.ToLower(filepath.Ext(outputPath)) == jsonExt

	if outputExplicit && outputPath == "" {
		if isJSON {
			outputPath = "gomajor-report.json"
		} else {
			outputPath = "gomajor-report.yaml"
		}
	}

	if outputPath != "" {
		return format.WriteReport(cfg.Out, outputPath, isJSON, results)
	}

	if cfg.JSONOutput {
		return format.PrintMultiJSONResults(cfg.Out, results)
	}

	format.PrintTextResults(cfg.Out, results, singleMode)
	return nil
}

func normalizeGitHubRepos(repos []string) []string {
	var normalized []string
	for _, repo := range repos {
		parts := utils.NormalizeSplitString(repo)
		normalized = append(normalized, parts...)
	}
	return normalized
}

func resolveConfigPath(cfg *config.Config, configExplicit bool) string {
	configPath := cfg.ConfigPath
	if !configExplicit {
		if _, err := os.Stat("gomajor.yaml"); err == nil {
			return "gomajor.yaml"
		} else if !os.IsNotExist(err) {
			if cfg.Logger != nil {
				cfg.Logger.Warn("failed to stat gomajor.yaml", "error", err)
			}
		}
	}
	return configPath
}
