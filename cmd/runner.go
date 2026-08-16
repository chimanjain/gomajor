package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/chimanjain/gomajor/pkg/model"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

const jsonExt = ".json"

// Runtime holds the parsed configuration and runtime dependencies (I/O, logger, HTTP clients).
type Runtime struct {
	Config           *config.Config
	YAMLConfig       config.YAMLConfig
	Client           checker.ModChecker
	GitHubHTTPClient *http.Client
	Out              io.Writer
	Err              io.Writer
	Logger           *slog.Logger
	OnProgress       func(completed, total int)
}

func runChecker(ctx context.Context, rt *Runtime, singleMode bool) error {
	if rt.Config == nil {
		rt.Config = config.DefaultConfig()
	}
	if rt.Out == nil {
		rt.Out = io.Discard
	}
	if rt.Err == nil {
		rt.Err = io.Discard
	}
	if rt.Client != nil {
		defer rt.Client.Close()
	}

	origNoColor := color.NoColor
	defer func() {
		color.NoColor = origNoColor
	}()
	if rt.Config.NoColor {
		color.NoColor = true
	}

	// GitHub repo normalisation and deduplication is handled inside
	// engine.RunMultiSources via normalizeSources. Pass raw values through.

	if rt.Err == os.Stderr && isatty.IsTerminal(os.Stderr.Fd()) && !rt.Config.JSONOutput && rt.OnProgress == nil {
		rt.OnProgress = func(completed, total int) {
			fmt.Fprintf(rt.Err, "\r%s [%d/%d]", color.HiBlackString("Checking dependencies..."), completed, total)
			if completed == total {
				fmt.Fprintln(rt.Err)
			}
		}
	}

	eng := engine.New(engine.Options{
		Client:           rt.Client,
		GitHubHTTPClient: rt.GitHubHTTPClient,
		MaxProbe:         rt.Config.MaxProbe,
		CheckAll:         rt.Config.CheckAll,
		Logger:           rt.Logger,
		OnProgress:       rt.OnProgress,
	})

	var results []model.SourceResult
	var err error
	if singleMode {
		results, err = executeSingleSource(ctx, eng, rt)
	} else {
		results, err = eng.RunMultiSources(ctx, rt.YAMLConfig)
	}
	if err != nil {
		return err
	}

	return writeOutput(rt, results, singleMode)
}

func executeSingleSource(ctx context.Context, eng *engine.Engine, rt *Runtime) ([]model.SourceResult, error) {
	path := rt.Config.ModFilePath
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

	if !rt.Config.JSONOutput && rt.Config.OutputPath == "" && len(results) > 0 {
		count := len(results[0].Dependencies)
		format.PrintAnalysisHeader(rt.Out, count, rt.Config.CheckAll, path)
	}
	return results, nil
}

func writeOutput(rt *Runtime, results []model.SourceResult, singleMode bool) error {
	outputPath := rt.Config.OutputPath
	isJSON := rt.Config.JSONOutput || strings.ToLower(filepath.Ext(outputPath)) == jsonExt

	if outputPath != "" {
		return format.WriteReport(rt.Out, outputPath, isJSON, results)
	}

	if rt.Config.JSONOutput {
		return format.PrintMultiJSONResults(rt.Out, results)
	}

	// Use rt.Config.Minor directly: the client was constructed with the correct
	// WithDisableMinor option, so rt.Config.Minor is the single source of truth.
	format.PrintTextResults(rt.Out, results, singleMode, !rt.Config.Minor)
	return nil
}
