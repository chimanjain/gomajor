package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chimanjain/gomajor/checker"
	"github.com/fatih/color"
)

func printJsonResults(results []checker.ModuleInfo) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printTextResults(results []checker.ModuleInfo) bool {
	var hasUpdates bool
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	headerColor := color.New(color.Bold, color.Underline).SprintFunc()

	first := true
	for _, info := range results {
		if info.HasUpdate {
			if first {
				fmt.Fprintln(w, headerColor("MODULE\tCURRENT\tLATEST\tNEW PATH"))
				first = false
			}
			hasUpdates = true
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				color.CyanString(info.BasePath),
				info.CurrentVersion,
				color.YellowString(info.LatestMajorVersion),
				color.HiBlackString(info.LatestMajorPath))
		}
	}
	w.Flush()
	if hasUpdates {
		fmt.Println()
	}
	return hasUpdates
}

func printMultiTextResults(results []SourceResult) {
	headerColor := color.New(color.Bold, color.Underline).SprintFunc()

	for i, res := range results {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s (%s)\n", color.HiCyanString(res.Source), color.HiBlackString(res.SourceType))
		if len(res.Dependencies) == 0 {
			fmt.Println("  No matching dependencies found.")
			continue
		}

		var hasUpdates bool
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		first := true
		for _, dep := range res.Dependencies {
			if dep.HasUpdate {
				if first {
					fmt.Fprintln(w, "  "+headerColor("MODULE\tCURRENT\tLATEST\tNEW PATH"))
					first = false
				}
				hasUpdates = true
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
					color.CyanString(dep.Module),
					dep.CurrentVersion,
					color.YellowString(dep.LatestMajorVersion),
					color.HiBlackString(dep.LatestMajorPath))
			}
		}
		w.Flush()

		if !hasUpdates {
			fmt.Println(color.GreenString("  ✔ All checked dependencies are on their latest major versions."))
		}
	}
}

// printAnalysisHeader prints the header showing what dependencies are being analyzed.
func printAnalysisHeader(count int, checkAll bool, path string) {
	msg := fmt.Sprintf("Analyzing %d direct dependencies", count)
	if checkAll {
		msg = fmt.Sprintf("Analyzing %d dependencies (direct and indirect)", count)
	}
	fmt.Printf("%s from %s...\n\n", color.HiCyanString(msg), color.HiBlackString(path))
}
