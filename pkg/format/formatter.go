package format

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/chimanjain/gomajor/internal/modpath"
	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/fatih/color"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/width"
)

// YAMLOutput defines the structured schema for the saved YAML output.
type YAMLOutput struct {
	Results []engine.SourceResult `yaml:"results" json:"results"`
}

func WriteReport(w io.Writer, outputPath string, isJSON bool, results []engine.SourceResult) error {
	cleanPath := filepath.Clean(outputPath)
	if fi, err := os.Lstat(cleanPath); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("refusing to write report to symlink: %s", cleanPath)
	}

	outputData := YAMLOutput{Results: results}
	formatName := "YAML"
	data, err := yaml.Marshal(outputData)
	if isJSON {
		formatName = "JSON"
		data, err = json.MarshalIndent(outputData, "", "  ")
	}

	if err == nil {
		err = os.WriteFile(cleanPath, data, 0o600)
	}
	if err != nil {
		return fmt.Errorf("failed to write %s output: %w", formatName, err)
	}
	fmt.Fprintf(w, "Results written to %s file: %s\n", formatName, cleanPath)
	return nil
}

func PrintMultiJSONResults(w io.Writer, results []engine.SourceResult) error {
	outputData := YAMLOutput{Results: results}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(outputData)
}

// visualLen returns the display width of a string in terminal columns.
// ANSI escape sequences are skipped (they occupy 0 columns), and East-Asian
// Wide / Fullwidth characters are counted as 2 columns each.
func visualLen(s string) int {
	var count int
	inEscape := false
	for i := 0; i < len(s); {
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			i++
			continue
		}
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			i += 2
			continue
		}
		if s[i] < utf8.RuneSelf {
			count++
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			count += 2
		default:
			count++
		}
		i += size
	}
	return count
}

const spacesStr = "                                                                                                                                                                                                        "

func padWithLen(s string, vlen, w int) string {
	if vlen >= w {
		return s
	}
	need := w - vlen
	if need <= len(spacesStr) {
		return s + spacesStr[:need]
	}
	return s + strings.Repeat(" ", need)
}

func pad(s string, w int) string {
	return padWithLen(s, visualLen(s), w)
}

func formatRow(mod, current, minorVer string, hasMinor bool, majorVer, majorPath string, hasMajor bool) []string {
	minor, major, newPath := "-", "-", "-"
	if hasMinor {
		minor = color.GreenString(minorVer)
	}
	if hasMajor {
		major, newPath = color.YellowString(majorVer), color.HiBlackString(majorPath)
	}
	return []string{color.CyanString(mod), current, minor, major, newPath}
}

func printTable(w io.Writer, indent string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	header := []string{"MODULE", "CURRENT", "MINOR", "MAJOR", "NEW PATH"}
	headerLens := [4]int{
		visualLen(header[0]),
		visualLen(header[1]),
		visualLen(header[2]),
		visualLen(header[3]),
	}

	rowLens := make([][4]int, len(rows))
	widths := make([]int, 5)
	for col := range 4 {
		maxW := headerLens[col]
		for i, row := range rows {
			wCol := visualLen(row[col])
			rowLens[i][col] = wCol
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
		headerColor(padWithLen(header[0], headerLens[0], widths[0])),
		headerColor(padWithLen(header[1], headerLens[1], widths[1])),
		headerColor(padWithLen(header[2], headerLens[2], widths[2])),
		headerColor(padWithLen(header[3], headerLens[3], widths[3])),
		headerColor(header[4]),
	)

	for i, row := range rows {
		_, _ = fmt.Fprintf(
			w, "%s%s%s%s%s%s\n",
			indent,
			padWithLen(row[0], rowLens[i][0], widths[0]),
			padWithLen(row[1], rowLens[i][1], widths[1]),
			padWithLen(row[2], rowLens[i][2], widths[2]),
			padWithLen(row[3], rowLens[i][3], widths[3]),
			row[4],
		)
	}
}

func printDependencies(w io.Writer, indent string, deps []engine.DependencyInfo, disableMinor bool) bool {
	var rows [][]string
	for _, dep := range deps {
		if dep.HasUpdate || dep.HasMinorUpdate {
			basePath, _, _ := modpath.ParseModulePath(dep.Module)
			rows = append(rows, formatRow(basePath, dep.CurrentVersion, dep.LatestMinorVersion, dep.HasMinorUpdate, dep.LatestMajorVersion, dep.LatestMajorPath, dep.HasUpdate))
		}
	}

	if len(rows) == 0 {
		msg := "✔ All checked dependencies are up to date."
		if disableMinor {
			msg = "✔ All checked dependencies are on their latest major versions."
		}
		_, _ = fmt.Fprintln(w, color.GreenString(indent+msg))
		return false
	}
	printTable(w, indent, rows)
	return true
}

func PrintTextResults(w io.Writer, results []engine.SourceResult, singleMode bool, disableMinor bool) {
	if singleMode {
		if len(results) == 0 {
			return
		}
		res := results[0]
		if len(res.Dependencies) == 0 {
			_, _ = fmt.Fprintln(w, "No matching dependencies found in", res.Source)
			return
		}

		if printDependencies(w, "", res.Dependencies, disableMinor) {
			_, _ = fmt.Fprintln(w)
		}
	} else {
		for i, res := range results {
			if i > 0 {
				_, _ = fmt.Fprintln(w)
			}
			_, _ = fmt.Fprintf(w, "%s (%s)\n", color.HiCyanString(res.Source), color.HiBlackString(string(res.SourceType)))

			if len(res.Dependencies) == 0 {
				_, _ = fmt.Fprintln(w, "  No matching dependencies found.")
				continue
			}

			printDependencies(w, "  ", res.Dependencies, disableMinor)
		}
	}
}

func PrintAnalysisHeader(w io.Writer, count int, checkAll bool, path string) {
	if w == nil {
		return
	}
	scope := "direct dependencies"
	if checkAll {
		scope = "dependencies (direct and indirect)"
	}
	_, _ = fmt.Fprintf(w, "%s from %s...\n\n", color.HiCyanString(fmt.Sprintf("Analyzing %d %s", count, scope)), color.HiBlackString(path))
}
