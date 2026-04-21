package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/chimanjain/gomajor/checker"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

var (
	modFilePath string
	maxProbe    int
	checkAll    bool
)

var rootCmd = &cobra.Command{
	Use:   "gomajor",
	Short: "Checks for major version updates of Go modules",
	Long: `A tool that parses a go.mod file and checks the Go proxy 
to discover if there are newer major versions (e.g. v2 -> v3) 
available for your dependencies.`,
	Run: func(cmd *cobra.Command, args []string) {
		runChecker(cmd.Flags().Changed("file"))
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&modFilePath, "file", "f", "", "Path to the go.mod file (default: auto-detect in current directory or binary directory)")
	rootCmd.Flags().IntVarP(&maxProbe, "max-probe", "m", 5, "Maximum number of subsequent major versions to probe for")
	rootCmd.Flags().BoolVarP(&checkAll, "all", "a", false, "Check all dependencies, including indirect ones (by default only direct dependencies are checked)")
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

func runChecker(fileExplicit bool) {
	// Resolve the path to go.mod.
	path := modFilePath
	if !fileExplicit {
		resolved, err := resolveModFile()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		path = resolved
	}

	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", path, err)
		os.Exit(1)
	}

	modFile, err := modfile.Parse(path, content, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		os.Exit(1)
	}

	var reqs []*modfile.Require
	for _, req := range modFile.Require {
		if !checkAll && req.Indirect {
			continue
		}
		reqs = append(reqs, req)
	}

	if len(reqs) == 0 {
		fmt.Println("No matching dependencies found in", path)
		return
	}

	if checkAll {
		fmt.Printf("Analyzing %d dependencies (direct and indirect) from %s...\n\n", len(reqs), path)
	} else {
		fmt.Printf("Analyzing %d direct dependencies from %s...\n\n", len(reqs), path)
	}

	var wg sync.WaitGroup
	results := make(chan checker.ModuleInfo, len(reqs))

	for _, req := range reqs {
		wg.Add(1)
		go func(modPath, version string) {
			defer wg.Done()
			info := checker.Check(modPath, version, maxProbe)
			results <- info
		}(req.Mod.Path, req.Mod.Version)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var hasUpdates bool

	for info := range results {
		if info.HasUpdate {
			hasUpdates = true
			fmt.Printf("UPDATE AVAILABLE: %s %s -> %s (path: %s)\n",
				info.Current,
				info.CurrentVersion,
				info.LatestMajorVersion,
				info.LatestMajorPath)
		}
	}

	if !hasUpdates {
		fmt.Println("All checked dependencies are on their latest major versions.")
	}
}
