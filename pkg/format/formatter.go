package format

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/chimanjain/gomajor/pkg/engine"
	"github.com/chimanjain/gomajor/utils"
	"github.com/fatih/color"
	"go.yaml.in/yaml/v3"
)

// YAMLOutput defines the structured schema for the saved YAML output.
type YAMLOutput struct {
	Results []engine.SourceResult `yaml:"results" json:"results"`
}

func WriteReport(w io.Writer, outputPath string, isJSON bool, results []engine.SourceResult) error {
	outputData := YAMLOutput{Results: results}
	var data []byte
	var err error
	formatName := "YAML"

	if isJSON {
		formatName = "JSON"
		data, err = json.MarshalIndent(outputData, "", "  ")
	} else {
		data, err = yaml.Marshal(outputData)
	}

	if err == nil {
		err = os.WriteFile(outputPath, data, 0o600)
	}
	if err != nil {
		return fmt.Errorf("failed to write %s output: %w", formatName, err)
	}
	fmt.Fprintf(w, "Results written to %s file: %s\n", formatName, outputPath)
	return nil
}

func PrintMultiJSONResults(w io.Writer, results []engine.SourceResult) error {
	outputData := YAMLOutput{Results: results}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(outputData)
}

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
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		count++
		i += size
	}
	return count
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
	for col := range 4 {
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

func printDependencies(w io.Writer, indent string, deps []engine.DependencyInfo) bool {
	var rows [][]string
	for _, dep := range deps {
		if dep.HasUpdate || dep.HasMinorUpdate {
			basePath, _, _ := utils.ParseModulePath(dep.Module)
			rows = append(rows, formatRow(basePath, dep.CurrentVersion, dep.LatestMinorVersion, dep.HasMinorUpdate, dep.LatestMajorVersion, dep.LatestMajorPath, dep.HasUpdate))
		}
	}

	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, color.GreenString(indent+"✔ All checked dependencies are on their latest major versions."))
		return false
	}
	printTable(w, indent, rows)
	return true
}

func PrintTextResults(w io.Writer, results []engine.SourceResult, singleMode bool) {
	if singleMode {
		if len(results) == 0 {
			return
		}
		res := results[0]
		if len(res.Dependencies) == 0 {
			_, _ = fmt.Fprintln(w, "No matching dependencies found in", res.Source)
			return
		}

		if printDependencies(w, "", res.Dependencies) {
			_, _ = fmt.Fprintln(w)
		}
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

			printDependencies(w, "  ", res.Dependencies)
		}
	}
}

func PrintAnalysisHeader(w io.Writer, count int, checkAll bool, path string) {
	if w == nil {
		return
	}
	msg := fmt.Sprintf("Analyzing %d direct dependencies", count)
	if checkAll {
		msg = fmt.Sprintf("Analyzing %d dependencies (direct and indirect)", count)
	}
	_, _ = fmt.Fprintf(w, "%s from %s...\n\n", color.HiCyanString(msg), color.HiBlackString(path))
}
