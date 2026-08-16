package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/constants"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is the current version of gomajor.
const Version = "v1.10.0"

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
			client := checker.DefaultClient(
				checker.WithDisableMinor(!cfg.Minor),
				checker.WithDisableMajor(!cfg.Major),
			)

			logLevel := slog.LevelInfo
			if cfg.Verbose {
				logLevel = slog.LevelDebug
			}
			handler := slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{
				Level: logLevel,
			})

			rt := &Runtime{
				Config:           cfg,
				YAMLConfig:       yamlCfg,
				Client:           client,
				GitHubHTTPClient: client.HTTPClient,
				Out:              cmd.OutOrStdout(),
				Err:              cmd.ErrOrStderr(),
				Logger:           slog.New(handler),
			}

			return runChecker(cmd.Context(), rt, isSingleMode(cfg, yamlCfg))
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

func Execute() {
	err := func() error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		return NewRootCmd().ExecuteContext(ctx)
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseConfig(cmd *cobra.Command) (*config.Config, config.YAMLConfig, error) {
	v := viper.New()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return nil, config.YAMLConfig{}, err
	}

	configPath := v.GetString("config")
	configExplicit := cmd.Flags().Changed("config")
	fileExplicit := cmd.Flags().Changed("file")

	if !configExplicit && configPath == "" && !fileExplicit {
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
	if cfg.MaxProbe > constants.MaxAllowedMaxProbe {
		cfg.MaxProbe = constants.MaxAllowedMaxProbe
	} else if cfg.MaxProbe < 1 {
		cfg.MaxProbe = constants.DefaultMaxProbe
	}
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
		if err := validateConfigOutputPath(fileYamlCfg.Output); err != nil {
			return nil, config.YAMLConfig{}, fmt.Errorf("invalid output path in config file %s: %w", configPath, err)
		}
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

// validateConfigOutputPath validates that output paths specified in configuration
// files do not attempt path traversal or write to arbitrary absolute paths.
func validateConfigOutputPath(outPath string) error {
	clean := filepath.Clean(outPath)
	if filepath.IsAbs(clean) {
		return fmt.Errorf("absolute path %q is not allowed in config file; specify a relative path or use the CLI --output flag", outPath)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traversal %q is not allowed in config file; output must reside within the workspace", outPath)
	}
	return nil
}

// isSingleMode reports whether the invocation targets a single local go.mod
// (no config file, no GitHub repos, no multi-source YAML entries). Extracting
// this predicate avoids duplicating the condition in both the command and tests.
func isSingleMode(cfg *config.Config, yamlCfg config.YAMLConfig) bool {
	return cfg.ConfigPath == "" && len(cfg.GitHubRepos) == 0 && len(yamlCfg.Local) == 0 && len(yamlCfg.Github) == 0
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
