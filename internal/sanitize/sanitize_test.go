package sanitize

import (
	"testing"
)

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "UserCredentials",
			input:    "https://token:x-oauth-basic@github.com/owner/repo/go.mod",
			expected: "https://redacted@github.com/owner/repo/go.mod",
		},
		{
			name:     "UserPassword",
			input:    "https://user:password@git.internal.corp/project/go.mod",
			expected: "https://redacted@git.internal.corp/project/go.mod",
		},
		{
			name:     "NoCredentials",
			input:    "https://github.com/owner/repo/go.mod",
			expected: "https://github.com/owner/repo/go.mod",
		},
		{
			name:     "LocalPath",
			input:    "/path/to/go.mod",
			expected: "/path/to/go.mod",
		},
		{
			name:     "TokenQueryParam",
			input:    "https://github.com/owner/repo?token=ghp_secret123",
			expected: "https://github.com/owner/repo?token=REDACTED",
		},
		{
			name:     "SchemelessCredentials",
			input:    "user:password@github.com/owner/repo",
			expected: "redacted@github.com/owner/repo",
		},
		{
			name:     "StandaloneGHPToken",
			input:    "failed to authenticate with token ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			expected: "failed to authenticate with token [REDACTED_TOKEN]",
		},
		{
			name:     "StandaloneBearerHeader",
			input:    "HTTP 401: Unauthorized for Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			expected: "HTTP 401: Unauthorized for Bearer [REDACTED_TOKEN]",
		},
		{
			name:     "FineGrainedPAT",
			input:    "error using github_pat_11AAAAAAA0123456789012345678901234567890123456789012345678901234567890123456789012",
			expected: "error using [REDACTED_TOKEN]",
		},
		{
			name:     "EmptyString",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeURL(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeTerminalString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantStr   string
		wantWidth int
	}{
		{"CleanASCII", "github.com/foo/bar", "github.com/foo/bar", 18},
		{"ANSIEscape", "\x1b[31;1mhello\x1b[0m", "hello", 5},
		{"ControlChars", "foo\r\nbar\a", "foobar", 6},
		{"WideUnicode", "👋 Go", "👋 Go", 5},
		{"BiDiStripped", "abc\u202Edef", "abcdef", 6},
		{"Empty", "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStr, gotWidth := SanitizeAndVisualLen(tt.input)
			if gotStr != tt.wantStr {
				t.Errorf("SanitizeAndVisualLen string = %q, want %q", gotStr, tt.wantStr)
			}
			if gotWidth != tt.wantWidth {
				t.Errorf("SanitizeAndVisualLen width = %d, want %d", gotWidth, tt.wantWidth)
			}

			if got := SanitizeTerminalString(tt.input); got != tt.wantStr {
				t.Errorf("SanitizeTerminalString = %q, want %q", got, tt.wantStr)
			}
			if got := VisualLen(tt.input); got != tt.wantWidth {
				t.Errorf("VisualLen = %d, want %d", got, tt.wantWidth)
			}
		})
	}
}

func BenchmarkSanitizeTerminalString_CleanASCII(b *testing.B) {
	s := "github.com/gin-gonic/gin/v2"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = SanitizeTerminalString(s)
	}
}

func BenchmarkSanitizeURL_Clean(b *testing.B) {
	u := "https://raw.githubusercontent.com/owner/repo/main/go.mod"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = SanitizeURL(u)
	}
}
