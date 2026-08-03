package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/chimanjain/gomajor/pkg/checker"
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

			// Build the client with the correct minor/major settings.
			// DefaultConfig leaves Client nil to avoid a wasted allocation.
			client := checker.DefaultClient(
				checker.WithDisableMinor(!cfg.Minor),
				checker.WithDisableMajor(!cfg.Major),
			)
			cfg.Client = client
			cfg.GitHubHTTPClient = client.HTTPClient

			logLevel := slog.LevelInfo
			if cfg.Verbose {
				logLevel = slog.LevelDebug
			}
			handler := slog.NewTextHandler(cfg.Err, &slog.HandlerOptions{
				Level: logLevel,
			})
			cfg.Logger = slog.New(handler)

			return runCheckerWithConfig(cmd.Context(), cfg, yamlCfg, isSingleMode(cfg, yamlCfg))
		},
	}

	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.Flags().StringP("file", "f", "", "Path to the go.mod file (default: auto-detect in current directory)")
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
			// Single viper instance: unmarshal the already-read config into the
			// YAML struct. A second viper.ReadInConfig call is not needed.
			if err := v.Unmarshal(&fileYamlCfg); err != nil {
				return nil, config.YAMLConfig{}, fmt.Errorf("parsing config file %s: %w", configPath, err)
			}
		}
	}

	cfg := config.DefaultConfig()
	cfg.ModFilePath = v.GetString("file")
	cfg.MaxProbe = v.GetInt("max-probe")
	cfg.CheckAll = v.GetBool("all")
	cfg.JSONOutput = v.GetBool("json")
	cfg.NoColor = v.GetBool("no-color")
	cfg.Verbose = v.GetBool("verbose")
	cfg.ConfigPath = configPath

	// Strict config merging: CLI flag (explicitly set) > YAML file > Defaults
	switch {
	case cmd.Flags().Changed("minor"):
		cfg.Minor = v.GetBool("minor")
	case fileYamlCfg.Minor != nil:
		cfg.Minor = *fileYamlCfg.Minor
	default:
		cfg.Minor = v.GetBool("minor")
	}

	switch {
	case cmd.Flags().Changed("major"):
		cfg.Major = v.GetBool("major")
	case fileYamlCfg.Major != nil:
		cfg.Major = *fileYamlCfg.Major
	default:
		cfg.Major = v.GetBool("major")
	}

	switch {
	case cmd.Flags().Changed("output"):
		cfg.OutputPath = v.GetString("output")
		if cfg.OutputPath == "" {
			if cfg.JSONOutput {
				cfg.OutputPath = "gomajor-report.json"
			} else {
				cfg.OutputPath = "gomajor-report.yaml"
			}
		}
	case fileYamlCfg.Output != "":
		cfg.OutputPath = fileYamlCfg.Output
	default:
		cfg.OutputPath = ""
	}

	cliGithub, _ := cmd.Flags().GetStringSlice("github")
	cfg.GitHubRepos = cliGithub

	yamlCfg := config.YAMLConfig{
		Local:  fileYamlCfg.Local,
		Github: append(fileYamlCfg.Github, cliGithub...),
		Output: cfg.OutputPath,
		Minor:  &cfg.Minor,
		Major:  &cfg.Major,
	}

	return cfg, yamlCfg, nil
}

// isSingleMode reports whether the invocation targets a single local go.mod
// (no config file, no GitHub repos, no multi-source YAML entries). Extracting
// this predicate avoids duplicating the condition in both the command and tests.
func isSingleMode(cfg *config.Config, yamlCfg config.YAMLConfig) bool {
	return cfg.ConfigPath == "" && len(cfg.GitHubRepos) == 0 && len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0
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
// the user did not explicitly pass --file. It checks the current working directory.
func resolveModFile() (string, error) {
	cwd, err := os.Getwd()
	if err == nil {
		candidate := filepath.Join(cwd, "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no go.mod found in current directory (%s); use --file to specify a path", cwd)
}
