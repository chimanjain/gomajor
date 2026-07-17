package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

const jsonExt = ".json"

func runCheckerWithConfig(ctx context.Context, cfg *config.Config, yamlCfg config.YAMLConfig, singleMode bool) error {
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

	// GitHub repo normalisation and deduplication is handled inside
	// engine.RunMultiSources via normalizeSources. Pass raw values through.

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
		results, err = eng.RunMultiSources(ctx, yamlCfg)
		if err != nil {
			return err
		}
	} else {
		results, err = executeSingleSource(ctx, eng, cfg)
		if err != nil {
			return err
		}
	}

	return writeOutput(cfg, results, singleMode)
}

func executeSingleSource(ctx context.Context, eng *engine.Engine, cfg *config.Config) ([]engine.SourceResult, error) {
	path := cfg.ModFilePath
	if path == "" {
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

	if !cfg.JSONOutput && cfg.OutputPath == "" && len(results) > 0 {
		count := len(results[0].Dependencies)
		format.PrintAnalysisHeader(cfg.Out, count, cfg.CheckAll, path)
	}
	return results, nil
}

func writeOutput(cfg *config.Config, results []engine.SourceResult, singleMode bool) error {
	outputPath := cfg.OutputPath
	isJSON := cfg.JSONOutput || strings.ToLower(filepath.Ext(outputPath)) == jsonExt

	if outputPath != "" {
		return format.WriteReport(cfg.Out, outputPath, isJSON, results)
	}

	if cfg.JSONOutput {
		return format.PrintMultiJSONResults(cfg.Out, results)
	}

	// Use cfg.Minor directly: the client was constructed with the correct
	// WithDisableMinor option, so cfg.Minor is the single source of truth.
	format.PrintTextResults(cfg.Out, results, singleMode, !cfg.Minor)
	return nil
}
