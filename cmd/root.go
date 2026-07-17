package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is the current version of gomajor.
const Version = "v1.8.0"

var rootCmd = NewRootCmd()

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gomajor",
		Version: Version,
		Short:   "Checks for major version updates of Go modules",
		Long: `A tool that parses a go.mod file and checks the Go proxy 
to discover if there are newer major versions (e.g. v2 -> v3) 
available for your dependencies.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, yamlCfg, err := parseConfig(cmd)
			if err != nil {
				return fmt.Errorf("error parsing configuration: %w", err)
			}
			cfg.Client.DisableMinor = !cfg.Minor
			cfg.Client.DisableMajor = !cfg.Major

			logLevel := slog.LevelInfo
			if cfg.Verbose {
				logLevel = slog.LevelDebug
			}
			handler := slog.NewTextHandler(cfg.Err, &slog.HandlerOptions{
				Level: logLevel,
			})
			cfg.Logger = slog.New(handler)

			singleMode := cfg.ConfigPath == "" && len(cfg.GitHubRepos) == 0 && len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0
			return runCheckerWithConfig(cmd.Context(), cfg, yamlCfg, singleMode)
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
	cmd.Flags().Bool("verbose", false, "Enable verbose/debug log output")
	cmd.Flags().StringP("config", "c", "", "Path to the YAML configuration file (default: auto-detects 'gomajor.yaml' in current directory)")
	cmd.Flags().StringP("output", "o", "", "Path to save results in YAML or JSON format (defaults to 'gomajor-report.yaml' or 'gomajor-report.json' if outputting to a file, otherwise printed to terminal)")
	cmd.Flags().StringSliceP("github", "g", nil, "Check GitHub repositories directly (comma or space-separated)")

	return cmd
}

func parseConfig(cmd *cobra.Command) (*config.Config, config.YAMLConfig, error) {
	v := viper.New()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return nil, config.YAMLConfig{}, err
	}

	configPath := v.GetString("config")
	configExplicit := cmd.Flags().Changed("config")

	if !configExplicit && configPath == "" {
		if _, err := os.Stat("gomajor.yaml"); err == nil {
			configPath = "gomajor.yaml"
		}
	}

	var fileYamlCfg config.YAMLConfig
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			var notFoundErr viper.ConfigFileNotFoundError
			isNotFound := errors.As(err, &notFoundErr) || os.IsNotExist(err)
			if configExplicit || !isNotFound {
				return nil, config.YAMLConfig{}, fmt.Errorf("reading config file %s: %w", configPath, err)
			}
		} else {
			vFile := viper.New()
			vFile.SetConfigFile(configPath)
			if err := vFile.ReadInConfig(); err == nil {
				if err := vFile.Unmarshal(&fileYamlCfg); err != nil {
					return nil, config.YAMLConfig{}, fmt.Errorf("parsing config file %s: %w", configPath, err)
				}
			}
		}
	}

	cfg := config.DefaultConfig()
	cfg.ModFilePath = v.GetString("file")
	cfg.MaxProbe = v.GetInt("max-probe")
	cfg.CheckAll = v.GetBool("all")
	cfg.JSONOutput = v.GetBool("json")
	cfg.NoColor = v.GetBool("no-color")
	cfg.Minor = v.GetBool("minor")
	cfg.Major = v.GetBool("major")
	cfg.Verbose = v.GetBool("verbose")
	cfg.ConfigPath = configPath
	cfg.OutputPath = v.GetString("output")
	if cmd.Flags().Changed("output") && cfg.OutputPath == "" {
		if cfg.JSONOutput {
			cfg.OutputPath = "gomajor-report.json"
		} else {
			cfg.OutputPath = "gomajor-report.yaml"
		}
	}

	cliGithub, _ := cmd.Flags().GetStringSlice("github")
	cfg.GitHubRepos = cliGithub

	yamlCfg := config.YAMLConfig{
		Local:  fileYamlCfg.Local,
		Github: append(fileYamlCfg.Github, cliGithub...),
		Output: cfg.OutputPath,
	}

	return cfg, yamlCfg, nil
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
