package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/spf13/cobra"
)

// Version is the current version of gomajor.
const Version = "v1.7.0"

var rootCmd = NewRootCmd()

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gomajor",
		Version: Version,
		Short:   "Checks for major version updates of Go modules",
		Long: `A tool that parses a go.mod file and checks the Go proxy 
to discover if there are newer major versions (e.g. v2 -> v3) 
available for your dependencies.`,
		Run: func(cmd *cobra.Command, _ []string) {
			cfg, err := parseConfig(cmd)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error parsing configuration:", err)
				os.Exit(1)
			}
			cfg.Client.DisableMinor = !cfg.Minor
			cfg.Client.DisableMajor = !cfg.Major

			handler := slog.NewTextHandler(cfg.Err, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})
			cfg.Logger = slog.New(handler)

			runChecker(cmd.Context(), cfg, cmd.Flags().Changed("file"), cmd.Flags().Changed("config"), cmd.Flags().Changed("output"))
		},
	}

	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.Flags().StringP("file", "f", "", "Path to the go.mod file (default: auto-detect in current directory or binary directory)")
	cmd.Flags().IntP("max-probe", "m", 5, "Maximum number of subsequent major versions to probe for")
	cmd.Flags().BoolP("all", "a", false, "Check all dependencies, including indirect ones (by default only direct dependencies are checked)")
	cmd.Flags().Bool("json", false, "Output results in JSON format")
	cmd.Flags().Bool("no-color", false, "Disable color output")
	cmd.Flags().Bool("minor", true, "Check for minor updates within the current major version")
	cmd.Flags().Bool("major", true, "Check for major version upgrades")
	cmd.Flags().StringP("config", "c", "", "Path to the YAML configuration file (default: auto-detects 'gomajor.yaml' in current directory)")
	cmd.Flags().StringP("output", "o", "", "Path to save results in YAML or JSON format (defaults to 'gomajor-report.yaml' or 'gomajor-report.json' if outputting to a file, otherwise printed to terminal)")
	cmd.Flags().StringSliceP("github", "g", nil, "Check GitHub repositories directly (comma or space-separated)")

	return cmd
}

func parseConfig(cmd *cobra.Command) (*config.Config, error) {
	cfg := config.DefaultConfig()
	var err error

	cfg.ModFilePath, err = cmd.Flags().GetString("file")
	if err != nil {
		return nil, err
	}
	cfg.MaxProbe, err = cmd.Flags().GetInt("max-probe")
	if err != nil {
		return nil, err
	}
	cfg.CheckAll, err = cmd.Flags().GetBool("all")
	if err != nil {
		return nil, err
	}
	cfg.JSONOutput, err = cmd.Flags().GetBool("json")
	if err != nil {
		return nil, err
	}
	cfg.NoColor, err = cmd.Flags().GetBool("no-color")
	if err != nil {
		return nil, err
	}
	cfg.Minor, err = cmd.Flags().GetBool("minor")
	if err != nil {
		return nil, err
	}
	cfg.Major, err = cmd.Flags().GetBool("major")
	if err != nil {
		return nil, err
	}
	cfg.ConfigPath, err = cmd.Flags().GetString("config")
	if err != nil {
		return nil, err
	}
	cfg.OutputPath, err = cmd.Flags().GetString("output")
	if err != nil {
		return nil, err
	}
	cfg.GitHubRepos, err = cmd.Flags().GetStringSlice("github")
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func Execute() {
	err := func() error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		return rootCmd.ExecuteContext(ctx)
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// resolveModFile returns the path to use for go.mod, auto-discovering it when
// the user did not explicitly pass --file. It checks:
//  1. The current working directory.
//  2. The directory that contains the running binary.
func resolveModFile() (string, error) {
	// 1. Current working directory.
	cwd, err := os.Getwd()
	if err == nil {
		candidate := filepath.Join(cwd, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 2. Directory of the binary itself.
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no go.mod found in current directory (%s) or binary directory; use --file to specify a path", cwd)
}
