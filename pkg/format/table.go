package format

import (
	"fmt"
	"io"
	"strings"

	"github.com/chimanjain/gomajor/internal/modpath"
	"github.com/chimanjain/gomajor/internal/sanitize"
	"github.com/chimanjain/gomajor/pkg/model"
	"github.com/fatih/color"
)

// SanitizeTerminalString strips ANSI escape sequences, ASCII control characters,
// and Unicode bidirectional / invisible formatting characters to prevent terminal injection.
func SanitizeTerminalString(s string) string {
	return sanitize.TerminalString(s)
}

// visualLen returns the display width of a string in terminal columns.
func visualLen(s string) int {
	return sanitize.VisualLen(s)
}

// sanitizeAndVisualLen strips escape sequences and computes display width.
func sanitizeAndVisualLen(s string) (string, int) {
	return sanitize.AndVisualLen(s)
}

const spacePadding = "                                                                                                    " // 100 spaces

func padWithLen(s string, vlen, w int) string {
	diff := w - vlen
	if diff <= 0 {
		return s
	}
	if diff <= len(spacePadding) {
		return s + spacePadding[:diff]
	}
	return s + strings.Repeat(" ", diff)
}

type formattedRow struct {
	cells [5]string
	vLens [4]int
}

var (
	tableHeader     = [5]string{"MODULE", "CURRENT", "MINOR", "MAJOR", "NEW PATH"}
	tableHeaderLens = [4]int{6, 7, 5, 5} // static display widths of MODULE, CURRENT, MINOR, MAJOR
)

func buildFormattedRow(mod, current, minorVer string, hasMinor bool, majorVer, majorPath string, hasMajor bool) formattedRow {
	mod, modVLen := sanitizeAndVisualLen(mod)
	current, curVLen := sanitizeAndVisualLen(current)

	minor := "-"
	minorVLen := 1
	if hasMinor {
		sanitized, vLen := sanitizeAndVisualLen(minorVer)
		minor = color.GreenString(sanitized)
		minorVLen = vLen
	}

	major, newPath := "-", "-"
	majorVLen := 1
	if hasMajor {
		sanitizedMaj, vLen := sanitizeAndVisualLen(majorVer)
		major = color.YellowString(sanitizedMaj)
		majorVLen = vLen
		sanitizedPath, _ := sanitizeAndVisualLen(majorPath)
		newPath = color.HiBlackString(sanitizedPath)
	}

	return formattedRow{
		cells: [5]string{color.CyanString(mod), current, minor, major, newPath},
		vLens: [4]int{modVLen, curVLen, minorVLen, majorVLen},
	}
}

func renderTable(w io.Writer, indent string, rows []formattedRow) {
	if len(rows) == 0 {
		return
	}

	widths := tableHeaderLens
	for _, row := range rows {
		for col := range 4 {
			widths[col] = max(widths[col], row.vLens[col])
		}
	}
	for col := range 4 {
		widths[col] += 3
	}

	headerColor := color.New(color.Bold, color.Underline).SprintFunc()
	_, _ = fmt.Fprintf(
		w, "%s%s%s%s%s%s\n",
		indent,
		headerColor(padWithLen(tableHeader[0], tableHeaderLens[0], widths[0])),
		headerColor(padWithLen(tableHeader[1], tableHeaderLens[1], widths[1])),
		headerColor(padWithLen(tableHeader[2], tableHeaderLens[2], widths[2])),
		headerColor(padWithLen(tableHeader[3], tableHeaderLens[3], widths[3])),
		headerColor(tableHeader[4]),
	)

	for _, row := range rows {
		_, _ = fmt.Fprintf(
			w, "%s%s%s%s%s%s\n",
			indent,
			padWithLen(row.cells[0], row.vLens[0], widths[0]),
			padWithLen(row.cells[1], row.vLens[1], widths[1]),
			padWithLen(row.cells[2], row.vLens[2], widths[2]),
			padWithLen(row.cells[3], row.vLens[3], widths[3]),
			row.cells[4],
		)
	}
}

func printDependencies(w io.Writer, indent string, deps []model.DependencyInfo, disableMinor bool) bool {
	var rows []formattedRow
	for _, dep := range deps {
		if dep.HasUpdate || dep.HasMinorUpdate {
			basePath := dep.BasePath
			if basePath == "" {
				basePath, _, _ = modpath.ParseModulePath(dep.Module)
			}
			rows = append(rows, buildFormattedRow(basePath, dep.CurrentVersion, dep.LatestMinorVersion, dep.HasMinorUpdate, dep.LatestMajorVersion, dep.LatestMajorPath, dep.HasUpdate))
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
	renderTable(w, indent, rows)
	return true
}

// PrintTextResults renders formatted dependency results to w.
func PrintTextResults(w io.Writer, results []model.SourceResult, singleMode bool, disableMinor bool) {
	for i, res := range results {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}

		indent := "  "
		if singleMode {
			indent = ""
		} else {
			source := SanitizeTerminalString(res.Source)
			sourceType := SanitizeTerminalString(string(res.SourceType))
			_, _ = fmt.Fprintf(w, "%s (%s)\n", color.HiCyanString(source), color.HiBlackString(sourceType))
		}

		if len(res.Dependencies) == 0 {
			if singleMode {
				_, _ = fmt.Fprintln(w, "No matching dependencies found in", SanitizeTerminalString(res.Source))
			} else {
				_, _ = fmt.Fprintln(w, "  No matching dependencies found.")
			}
			continue
		}

		if printDependencies(w, indent, res.Dependencies, disableMinor) && singleMode {
			_, _ = fmt.Fprintln(w)
		}
	}
}

// PrintAnalysisHeader displays the analyzing dependencies banner.
func PrintAnalysisHeader(w io.Writer, count int, checkAll bool, path string) {
	if w == nil {
		return
	}
	path = SanitizeTerminalString(path)
	scope := "direct dependencies"
	if checkAll {
		scope = "dependencies (direct and indirect)"
	}
	_, _ = fmt.Fprintf(w, "%s from %s...\n\n", color.HiCyanString(fmt.Sprintf("Analyzing %d %s", count, scope)), color.HiBlackString(path))
}
