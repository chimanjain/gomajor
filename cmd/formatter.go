package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/chimanjain/gomajor/utils"
	"github.com/fatih/color"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visualLen(s string) int {
	cleaned := ansiRegex.ReplaceAllString(s, "")
	return utf8.RuneCountInString(cleaned)
}

func pad(s string, width int) string {
	vlen := visualLen(s)
	if vlen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vlen)
}

func formatRow(mod, current, minorVer string, hasMinor bool, majorVer, majorPath string, hasMajor bool) []string {
	minor := "-"
	if hasMinor {
		minor = color.GreenString(minorVer)
	}
	major := "-"
	newPath := "-"
	if hasMajor {
		major = color.YellowString(majorVer)
		newPath = color.HiBlackString(majorPath)
	}
	return []string{color.CyanString(mod), current, minor, major, newPath}
}

func printTable(w io.Writer, indent string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	header := []string{"MODULE", "CURRENT", "MINOR", "MAJOR", "NEW PATH"}
	widths := make([]int, 5)
	for col := 0; col < 4; col++ {
		maxW := visualLen(header[col])
		for _, row := range rows {
			wCol := visualLen(row[col])
			if wCol > maxW {
				maxW = wCol
			}
		}
		widths[col] = maxW + 3
	}

	headerColor := color.New(color.Bold, color.Underline).SprintFunc()
	_, _ = fmt.Fprintf(
		w, "%s%s%s%s%s%s\n",
		indent,
		headerColor(pad(header[0], widths[0])),
		headerColor(pad(header[1], widths[1])),
		headerColor(pad(header[2], widths[2])),
		headerColor(pad(header[3], widths[3])),
		headerColor(header[4]),
	)

	for _, row := range rows {
		_, _ = fmt.Fprintf(
			w, "%s%s%s%s%s%s\n",
			indent,
			pad(row[0], widths[0]),
			pad(row[1], widths[1]),
			pad(row[2], widths[2]),
			pad(row[3], widths[3]),
			row[4],
		)
	}
}

func printTextResults(w io.Writer, results []SourceResult, singleMode bool) {
	if singleMode {
		if len(results) == 0 {
			return
		}
		res := results[0]
		if len(res.Dependencies) == 0 {
			_, _ = fmt.Fprintln(w, "No matching dependencies found in", res.Source)
			return
		}

		var rows [][]string
		for _, dep := range res.Dependencies {
			if dep.HasUpdate || dep.HasMinorUpdate {
				basePath, _, _ := utils.ParseModulePath(dep.Module)
				rows = append(rows, formatRow(basePath, dep.CurrentVersion, dep.LatestMinorVersion, dep.HasMinorUpdate, dep.LatestMajorVersion, dep.LatestMajorPath, dep.HasUpdate))
			}
		}

		if len(rows) == 0 {
			_, _ = fmt.Fprintln(w, color.GreenString("✔ All checked dependencies are on their latest major versions."))
			return
		}
		printTable(w, "", rows)
		_, _ = fmt.Fprintln(w)
	} else {
		for i, res := range results {
			if i > 0 {
				_, _ = fmt.Fprintln(w)
			}
			_, _ = fmt.Fprintf(w, "%s (%s)\n", color.HiCyanString(res.Source), color.HiBlackString(res.SourceType))
			if len(res.Dependencies) == 0 {
				_, _ = fmt.Fprintln(w, "  No matching dependencies found.")
				continue
			}

			var rows [][]string
			for _, dep := range res.Dependencies {
				if dep.HasUpdate || dep.HasMinorUpdate {
					rows = append(rows, formatRow(dep.Module, dep.CurrentVersion, dep.LatestMinorVersion, dep.HasMinorUpdate, dep.LatestMajorVersion, dep.LatestMajorPath, dep.HasUpdate))
				}
			}

			if len(rows) == 0 {
				_, _ = fmt.Fprintln(w, color.GreenString("  ✔ All checked dependencies are on their latest major versions."))
				continue
			}

			printTable(w, "  ", rows)
		}
	}
}

func printAnalysisHeader(w io.Writer, count int, checkAll bool, path string) {
	msg := fmt.Sprintf("Analyzing %d direct dependencies", count)
	if checkAll {
		msg = fmt.Sprintf("Analyzing %d dependencies (direct and indirect)", count)
	}
	_, _ = fmt.Fprintf(w, "%s from %s...\n\n", color.HiCyanString(msg), color.HiBlackString(path))
}

func printMultiJSONResults(w io.Writer, results []SourceResult) error {
	outputData := YAMLOutput{Results: results}
	data, err := json.MarshalIndent(outputData, "", "  ")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w, string(data))
	return nil
}
