package format

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/pkg/model"
	"github.com/chimanjain/gomajor/pkg/source"
	"github.com/fatih/color"
)

func TestFormatter(t *testing.T) {
	t.Run("VisualLen", func(t *testing.T) {
		tests := []struct {
			input string
			want  int
		}{
			{"hello", 5},
			{"", 0},
			{color.GreenString("hello"), 5},
			{color.RedString("world") + "!", 6},
			{"👋 Go", 5}, // Hand wave emoji (East_Asian_Width=W) counts as 2 cols + space + G + o = 5
		}

		for _, tt := range tests {
			got := visualLen(tt.input)
			if got != tt.want {
				t.Errorf("visualLen(%q) = %d, want %d", tt.input, got, tt.want)
			}
		}
	})

	t.Run("PadWithLen", func(t *testing.T) {
		tests := []struct {
			input string
			width int
			want  string
		}{
			{"hello", 10, "hello     "},
			{"hello", 3, "hello"},
			{color.GreenString("hi"), 5, color.GreenString("hi") + "   "},
		}

		for _, tt := range tests {
			got := padWithLen(tt.input, visualLen(tt.input), tt.width)
			if got != tt.want {
				t.Errorf("padWithLen(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
			}
		}
	})

	t.Run("FormatRow", func(t *testing.T) {
		row := formatRow("github.com/foo/bar", "v1.0.0", "v1.1.0", true, "v2.0.0", "github.com/foo/bar/v2", true)
		if len(row) != 5 {
			t.Fatalf("expected 5 columns, got %d", len(row))
		}
		if !strings.Contains(row[0], "github.com/foo/bar") {
			t.Errorf("expected module in row[0], got %s", row[0])
		}
		if row[1] != "v1.0.0" {
			t.Errorf("expected current version in row[1], got %s", row[1])
		}
		if !strings.Contains(row[2], "v1.1.0") {
			t.Errorf("expected minor version in row[2], got %s", row[2])
		}
		if !strings.Contains(row[3], "v2.0.0") {
			t.Errorf("expected major version in row[3], got %s", row[3])
		}
		if !strings.Contains(row[4], "github.com/foo/bar/v2") {
			t.Errorf("expected major path in row[4], got %s", row[4])
		}
	})

	t.Run("SanitizeTerminalString", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"github.com/foo/bar", "github.com/foo/bar"},
			{"\x1b[2J\x1b[Hmalicious/pkg", "malicious/pkg"},
			{"\x1b[31;1mcolored/pkg\x1b[0m", "colored/pkg"},
			{"pkg\r\nwith\a\bcontrols", "pkgwithcontrols"},
			{"unicode/模块/v2", "unicode/模块/v2"},
			{"", ""},
		}

		for _, tt := range tests {
			got := sanitizeTerminalString(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeTerminalString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		}
	})

	t.Run("PrintTable_Empty", func(_ *testing.T) {
		// Just ensure it doesn't panic
		printTable(io.Discard, "", nil)
	})

	t.Run("PrintTable_Rows", func(t *testing.T) {
		rows := [][]string{
			{"mod", "v1.0.0", "v1.1.0", "v2.0.0", "mod/v2"},
		}

		var buf bytes.Buffer
		printTable(&buf, "  ", rows)
		out := buf.String()

		if !strings.Contains(out, "MODULE") || !strings.Contains(out, "mod/v2") {
			t.Errorf("unexpected table output: %q", out)
		}
	})
}

func TestPrintMultiJsonResults(t *testing.T) {
	results := []model.SourceResult{
		{
			Source:     "test-source",
			SourceType: source.Local,
			Dependencies: []model.DependencyInfo{
				{
					Module:             "github.com/foo/bar",
					CurrentVersion:     "v1.0.0",
					LatestMajorVersion: "v2.0.0",
					LatestMajorPath:    "github.com/foo/bar/v2",
					HasUpdate:          true,
				},
			},
		},
	}

	var buf bytes.Buffer
	err := PrintMultiJSONResults(&buf, results)
	if err != nil {
		t.Fatalf("PrintMultiJSONResults failed: %v", err)
	}

	var output YAMLOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal stdout JSON output: %v", err)
	}

	if len(output.Results) != 1 || output.Results[0].Source != "test-source" {
		t.Errorf("unexpected output struct: %+v", output)
	}
}

func TestWriteReport_Success(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "subdir", "report.json")
	results := []model.SourceResult{
		{
			Source:     "test-source",
			SourceType: source.Local,
			Dependencies: []model.DependencyInfo{
				{
					Module:         "github.com/foo/bar",
					CurrentVersion: "v1.0.0",
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteReport(&buf, outPath, true, results); err != nil {
		t.Fatalf("WriteReport failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read report: %v", err)
	}
	if !strings.Contains(string(data), "github.com/foo/bar") {
		t.Errorf("expected report to contain module, got: %s", string(data))
	}
}

func TestWriteReport_Symlink(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.json")
	if err := os.WriteFile(targetFile, []byte("target"), 0o600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	symlinkPath := filepath.Join(tmpDir, "symlink.json")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	err := WriteReport(io.Discard, symlinkPath, true, nil)
	if err == nil {
		t.Error("WriteReport should fail when target is a symlink")
	} else if !strings.Contains(err.Error(), "refusing to write report to symlink") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWriteReport_Errors(t *testing.T) {
	t.Run("InvalidDirectory", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create a regular file and then try to use it as a directory parent
		blockingFile := filepath.Join(tmpDir, "blocking")
		if err := os.WriteFile(blockingFile, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		badPath := filepath.Join(blockingFile, "subdir", "report.json")

		err := WriteReport(io.Discard, badPath, true, nil)
		if err == nil {
			t.Error("expected error when directory creation fails, got nil")
		}
	})
}

func TestPrintAnalysisHeader(t *testing.T) {
	t.Run("NilWriter", func(_ *testing.T) {
		// Should not panic on nil writer
		PrintAnalysisHeader(nil, 5, false, "go.mod")
	})

	t.Run("DirectDependencies", func(t *testing.T) {
		var buf bytes.Buffer
		PrintAnalysisHeader(&buf, 10, false, "go.mod")
		out := buf.String()
		if !strings.Contains(out, "Analyzing 10 direct dependencies") {
			t.Errorf("unexpected output: %s", out)
		}
		if !strings.Contains(out, "go.mod") {
			t.Errorf("expected path in output: %s", out)
		}
	})

	t.Run("AllDependencies", func(t *testing.T) {
		var buf bytes.Buffer
		PrintAnalysisHeader(&buf, 20, true, "sub/go.mod")
		out := buf.String()
		if !strings.Contains(out, "Analyzing 20 dependencies (direct and indirect)") {
			t.Errorf("unexpected output: %s", out)
		}
	})
}

func TestPrintTextResults(t *testing.T) {
	t.Run("SingleMode_EmptyResults", func(t *testing.T) {
		var buf bytes.Buffer
		PrintTextResults(&buf, nil, true, false)
		if buf.Len() != 0 {
			t.Errorf("expected empty output, got: %q", buf.String())
		}
	})

	t.Run("SingleMode_ZeroDependencies", func(t *testing.T) {
		var buf bytes.Buffer
		results := []model.SourceResult{
			{
				Source:       "go.mod",
				SourceType:   source.Local,
				Dependencies: nil,
			},
		}
		PrintTextResults(&buf, results, true, false)
		if !strings.Contains(buf.String(), "No matching dependencies found in go.mod") {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})

	t.Run("SingleMode_AllUpToDate_MinorEnabled", func(t *testing.T) {
		var buf bytes.Buffer
		results := []model.SourceResult{
			{
				Source:     "go.mod",
				SourceType: source.Local,
				Dependencies: []model.DependencyInfo{
					{
						Module:         "github.com/foo/bar",
						CurrentVersion: "v1.0.0",
						HasUpdate:      false,
						HasMinorUpdate: false,
					},
				},
			},
		}
		PrintTextResults(&buf, results, true, false)
		if !strings.Contains(buf.String(), "All checked dependencies are up to date") {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})

	t.Run("SingleMode_AllUpToDate_MinorDisabled", func(t *testing.T) {
		var buf bytes.Buffer
		results := []model.SourceResult{
			{
				Source:     "go.mod",
				SourceType: source.Local,
				Dependencies: []model.DependencyInfo{
					{
						Module:         "github.com/foo/bar",
						CurrentVersion: "v1.0.0",
						HasUpdate:      false,
						HasMinorUpdate: false,
					},
				},
			},
		}
		PrintTextResults(&buf, results, true, true)
		if !strings.Contains(buf.String(), "All checked dependencies are on their latest major versions") {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})

	t.Run("SingleMode_WithUpdates", func(t *testing.T) {
		var buf bytes.Buffer
		results := []model.SourceResult{
			{
				Source:     "go.mod",
				SourceType: source.Local,
				Dependencies: []model.DependencyInfo{
					{
						Module:             "github.com/foo/bar/v2",
						CurrentVersion:     "v2.0.0",
						LatestMajorVersion: "v3.0.0",
						LatestMajorPath:    "github.com/foo/bar/v3",
						HasUpdate:          true,
						LatestMinorVersion: "v2.1.0",
						HasMinorUpdate:     true,
					},
				},
			},
		}
		PrintTextResults(&buf, results, true, false)
		out := buf.String()
		if !strings.Contains(out, "MODULE") || !strings.Contains(out, "github.com/foo/bar") {
			t.Errorf("unexpected output: %q", out)
		}
	})

	t.Run("MultiMode_MultipleSources", func(t *testing.T) {
		var buf bytes.Buffer
		results := []model.SourceResult{
			{
				Source:       "local/go.mod",
				SourceType:   source.Local,
				Dependencies: nil,
			},
			{
				Source:     "https://github.com/owner/repo",
				SourceType: source.GitHub,
				Dependencies: []model.DependencyInfo{
					{
						Module:             "github.com/foo/baz",
						CurrentVersion:     "v1.0.0",
						LatestMajorVersion: "v2.0.0",
						LatestMajorPath:    "github.com/foo/baz/v2",
						HasUpdate:          true,
					},
				},
			},
		}
		PrintTextResults(&buf, results, false, false)
		out := buf.String()
		if !strings.Contains(out, "local/go.mod") || !strings.Contains(out, "No matching dependencies found.") {
			t.Errorf("expected local source with no deps in output: %q", out)
		}
		if !strings.Contains(out, "github.com/owner/repo") || !strings.Contains(out, "github.com/foo/baz") {
			t.Errorf("expected github source with updates in output: %q", out)
		}
	})
}
