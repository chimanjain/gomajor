package format

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/chimanjain/gomajor/pkg/engine"
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

	t.Run("Pad", func(t *testing.T) {
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
			got := pad(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("pad(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
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
	results := []engine.SourceResult{
		{
			Source:     "test-source",
			SourceType: source.Local,
			Dependencies: []engine.DependencyInfo{
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
