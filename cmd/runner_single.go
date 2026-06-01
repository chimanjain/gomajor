package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"golang.org/x/mod/modfile"
)

func runSingleChecker(cfg *Config, fileExplicit bool) error {
	// Resolve the path to go.mod.
	path := cfg.ModFilePath
	if !fileExplicit {
		resolved, err := resolveModFile()
		if err != nil {
			return err
		}
		path = resolved
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", path, err)
	}

	modFile, err := modfile.Parse(path, content, nil)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	var reqs []*modfile.Require
	for _, req := range modFile.Require {
		if !cfg.CheckAll && req.Indirect {
			continue
		}
		reqs = append(reqs, req)
	}

	if len(reqs) == 0 {
		if !cfg.JsonOutput {
			fmt.Println("No matching dependencies found in", path)
		} else {
			fmt.Println("[]")
		}
		return nil
	}

	if !cfg.JsonOutput {
		printAnalysisHeader(len(reqs), cfg.CheckAll, path)
	}

	allResults := checkDependencies(cfg, reqs)

	if cfg.JsonOutput {
		return printJsonResults(allResults)
	}

	hasUpdates := printTextResults(allResults)
	if !hasUpdates {
		fmt.Println(color.GreenString("✔ All checked dependencies are on their latest major versions."))
	}
	return nil
}
