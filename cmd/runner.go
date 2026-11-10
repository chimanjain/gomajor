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
	"sync"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/chimanjain/gomajor/pkg/model"
	"github.com/chimanjain/gomajor/pkg/source"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

const jsonExt = ".json"

// Runtime holds the parsed configuration and runtime dependencies (I/O, logger, HTTP clients).
type Runtime struct {
	Config           *config.Config
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
		var mu sync.Mutex
		prefix := color.HiBlackString("Checking dependencies...")
		rt.OnProgress = func(completed, total int) {
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(rt.Err, "\r%s [%d/%d]", prefix, completed, total)
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
		path := rt.Config.ModFilePath
		if path == "" {
			if path, err = resolveModFile(); err != nil {
				return err
			}
		}
		var parsed source.ParsedSource
		if parsed, err = source.ParseLocalMod(path); err != nil {
			return err
		}
		if !rt.Config.JSONOutput && rt.Config.OutputPath == "" {
			count := 0
			for _, req := range parsed.Reqs {
				if rt.Config.CheckAll || !req.Indirect {
					count++
				}
			}
			format.PrintAnalysisHeader(rt.Out, count, rt.Config.CheckAll, path)
		}
		results, err = eng.RunParsedSources(ctx, []source.ParsedSource{parsed})
	} else {
		results, err = eng.RunMultiSources(ctx, config.YAMLConfig{
			Local:  rt.Config.LocalMods,
			Github: rt.Config.GitHubRepos,
		})
	}
	if err != nil {
		return err
	}

	return writeOutput(rt, results, singleMode)
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
