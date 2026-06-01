package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chimanjain/gomajor/checker"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// DefaultConfig returns a config with standard settings.
func DefaultConfig() *Config {
	return &Config{
		ModFilePath: "",
		MaxProbe:    5,
		CheckAll:    false,
		JsonOutput:  false,
		NoColor:     false,
		Minor:       true,
		Major:       true,
		Client:      checker.DefaultClient(),
		ConfigPath:  "",
		OutputPath:  "",
		GithubRepos: nil,
	}
}

var config = DefaultConfig()

var rootCmd = &cobra.Command{
	Use:   "gomajor",
	Short: "Checks for major version updates of Go modules",
	Long: `A tool that parses a go.mod file and checks the Go proxy 
to discover if there are newer major versions (e.g. v2 -> v3) 
available for your dependencies.`,
	Run: func(cmd *cobra.Command, args []string) {
		if config.NoColor {
			color.NoColor = true
		}
		config.Client.DisableMinor = !config.Minor
		config.Client.DisableMajor = !config.Major
		runChecker(cmd.Flags().Changed("file"), cmd.Flags().Changed("config"), cmd.Flags().Changed("output"))
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&config.ModFilePath, "file", "f", "", "Path to the go.mod file (default: auto-detect in current directory or binary directory)")
	rootCmd.Flags().IntVarP(&config.MaxProbe, "max-probe", "m", 5, "Maximum number of subsequent major versions to probe for")
	rootCmd.Flags().BoolVarP(&config.CheckAll, "all", "a", false, "Check all dependencies, including indirect ones (by default only direct dependencies are checked)")
	rootCmd.Flags().BoolVar(&config.JsonOutput, "json", false, "Output results in JSON format")
	rootCmd.Flags().BoolVar(&config.NoColor, "no-color", false, "Disable color output")
	rootCmd.Flags().BoolVar(&config.Minor, "minor", true, "Check for minor updates within the current major version")
	rootCmd.Flags().BoolVar(&config.Major, "major", true, "Check for major version upgrades")
	rootCmd.Flags().StringVarP(&config.ConfigPath, "config", "c", "", "Path to the YAML configuration file (default: auto-detects 'gomajor.yaml' in current directory)")
	rootCmd.Flags().StringVarP(&config.OutputPath, "output", "o", "", "Path to save YAML results (defaults to 'gomajor-report.yaml' if outputting to a file, otherwise printed to terminal)")
	rootCmd.Flags().StringSliceVarP(&config.GithubRepos, "github", "g", nil, "Check GitHub repositories directly (comma-separated)")
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
