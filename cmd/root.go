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
	"syscall"

	"github.com/chimanjain/gomajor/pkg/checker"
	"github.com/chimanjain/gomajor/pkg/config"
	"github.com/chimanjain/gomajor/pkg/format"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v4"
)

// Version is the current version of gomajor.
var Version = "v1.11.0"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "gomajor",
		Version:       Version,
		SilenceErrors: true,
		Short:         "Checks for major version updates of Go modules",
		Long: `A tool that parses a go.mod file and checks the Go proxy 
to discover if there are newer major versions (e.g. v2 -> v3) 
available for your dependencies.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := parseConfig(cmd)
			if err != nil {
				return fmt.Errorf("error parsing configuration: %w", err)
			}

			if !cfg.Minor && !cfg.Major {
				return errors.New("cannot disable both minor and major checks; at least one must be enabled")
			}

			// Build the client with the correct minor/major settings.
			client := checker.DefaultClient(
				checker.WithDisableMinor(!cfg.Minor),
				checker.WithDisableMajor(!cfg.Major),
				checker.WithBaseContext(cmd.Context()),
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
				Client:           client,
				GitHubHTTPClient: client.HTTPClient,
				Out:              cmd.OutOrStdout(),
				Err:              cmd.ErrOrStderr(),
				Logger:           slog.New(handler),
			}

			return runChecker(cmd.Context(), rt, cfg.IsSingleMode())
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
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return NewRootCmd().ExecuteContext(ctx)
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, format.SanitizeTerminalString(err.Error()))
		os.Exit(1)
	}
}

func parseConfig(cmd *cobra.Command) (*config.Config, error) {
	configPath, _ := cmd.Flags().GetString("config")
	configExplicit := cmd.Flags().Changed("config")
	fileExplicit := cmd.Flags().Changed("file")

	if !configExplicit && configPath == "" && !fileExplicit {
		if _, err := os.Stat("gomajor.yaml"); err == nil {
			configPath = "gomajor.yaml"
		}
	}

	var fileYamlCfg config.YAMLConfig
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			if configExplicit || !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config file %s: %w", configPath, err)
			}
		} else {
			if err := yaml.Unmarshal(data, &fileYamlCfg); err != nil {
				return nil, fmt.Errorf("parsing config file %s: %w", configPath, err)
			}
		}
	}

	cfg := config.DefaultConfig()
	filePath, _ := cmd.Flags().GetString("file")
	cfg.ModFilePath = filePath
	cfg.ConfigPath = configPath

	// Strict config merging: CLI flag (explicitly set) > YAML file > Defaults
	mergeBool := func(flag string, fileVal *bool) bool {
		if cmd.Flags().Changed(flag) {
			val, _ := cmd.Flags().GetBool(flag)
			return val
		}
		if fileVal != nil {
			return *fileVal
		}
		val, _ := cmd.Flags().GetBool(flag)
		return val
	}

	mergeInt := func(flag string, fileVal *int) int {
		if cmd.Flags().Changed(flag) {
			val, _ := cmd.Flags().GetInt(flag)
			return val
		}
		if fileVal != nil {
			return *fileVal
		}
		val, _ := cmd.Flags().GetInt(flag)
		return val
	}

	cfg.CheckAll = mergeBool("all", fileYamlCfg.All)
	cfg.JSONOutput = mergeBool("json", fileYamlCfg.JSON)
	cfg.NoColor = mergeBool("no-color", fileYamlCfg.NoColor)
	cfg.Verbose = mergeBool("verbose", fileYamlCfg.Verbose)
	cfg.Minor = mergeBool("minor", fileYamlCfg.Minor)
	cfg.Major = mergeBool("major", fileYamlCfg.Major)

	cfg.MaxProbe = mergeInt("max-probe", fileYamlCfg.MaxProbe)
	if cfg.MaxProbe > config.MaxAllowedMaxProbe {
		cfg.MaxProbe = config.MaxAllowedMaxProbe
	} else if cfg.MaxProbe < 1 {
		cfg.MaxProbe = config.DefaultMaxProbe
	}

	switch {
	case cmd.Flags().Changed("output"):
		outVal, _ := cmd.Flags().GetString("output")
		cfg.OutputPath = outVal
		if cfg.OutputPath == "" {
			if cfg.JSONOutput {
				cfg.OutputPath = "gomajor-report.json"
			} else {
				cfg.OutputPath = "gomajor-report.yaml"
			}
		}
	case fileYamlCfg.Output != "":
		if err := validateConfigOutputPath(fileYamlCfg.Output); err != nil {
			return nil, fmt.Errorf("invalid output path in config file %s: %w", configPath, err)
		}
		cfg.OutputPath = fileYamlCfg.Output
	default:
		cfg.OutputPath = ""
	}

	cliGithub, _ := cmd.Flags().GetStringSlice("github")
	cfg.LocalMods = fileYamlCfg.Local
	cfg.GitHubRepos = append(cfg.GitHubRepos, fileYamlCfg.Github...)
	cfg.GitHubRepos = append(cfg.GitHubRepos, cliGithub...)

	return cfg, nil
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

// isSingleMode reports whether the invocation targets a single local go.mod.
func isSingleMode(cfg *config.Config) bool {
	return cfg.IsSingleMode()
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
