package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chimanjain/gomajor/internal/testutil"
)

var (
	_ Provider = LocalProvider{}
	_ Provider = GitHubProvider{}
)

func TestSanitizeURL(t *testing.T) {
	// Verify that source.SanitizeURL delegates properly to internal/sanitize.
	const raw = "https://token:secret@github.com/owner/repo"
	const want = "https://redacted@github.com/owner/repo"
	if got := SanitizeURL(raw); got != want {
		t.Errorf("SanitizeURL(%q) = %q, want %q", raw, got, want)
	}
}

func TestParseLocalMod(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		dir := t.TempDir()
		p := testutil.WriteModFile(t, dir, "module example.com/test\n\ngo 1.21\n\nrequire github.com/foo/bar v1.0.0\n")

		ps, err := ParseLocalMod(p)
		if err != nil {
			t.Fatalf("ParseLocalMod failed: %v", err)
		}
		if ps.Source != p || ps.SourceType != Local {
			t.Errorf("unexpected parsed source: %+v", ps)
		}
		if len(ps.Reqs) != 1 || ps.Reqs[0].Mod.Path != "github.com/foo/bar" {
			t.Errorf("unexpected requirements: %+v", ps.Reqs)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		if _, err := ParseLocalMod("/nonexistent/path/go.mod"); err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})

	t.Run("NonRegularFile_Directory", func(t *testing.T) {
		if _, err := ParseLocalMod(t.TempDir()); err == nil {
			t.Error("expected error parsing directory as go.mod, got nil")
		}
	})
}

func TestProviderParse(t *testing.T) {
	dir := t.TempDir()
	content := "module example.com/test\ngo 1.21\nrequire github.com/foo/bar v1.0.0\n"
	p := testutil.WriteModFile(t, dir, content)

	t.Run("LocalProvider_MethodsAndParse", func(t *testing.T) {
		lp := NewLocalProvider(p)
		if lp.Name() != p {
			t.Errorf("lp.Name() = %q, want %q", lp.Name(), p)
		}
		if lp.Type() != Local {
			t.Errorf("lp.Type() = %q, want %q", lp.Type(), Local)
		}

		ps, err := lp.Parse(context.Background(), nil)
		if err != nil {
			t.Fatalf("lp.Parse failed: %v", err)
		}
		if ps.Source != p || ps.SourceType != Local {
			t.Errorf("unexpected parsed source from LocalProvider: %+v", ps)
		}
		if len(ps.Reqs) != 1 || ps.Reqs[0].Mod.Path != "github.com/foo/bar" {
			t.Errorf("unexpected requirements from LocalProvider: %+v", ps.Reqs)
		}
	})

	t.Run("GitHubProvider_Methods", func(t *testing.T) {
		gp := NewGitHubProvider("https://token:secret@github.com/owner/repo")
		if gp.Name() != "https://redacted@github.com/owner/repo" {
			t.Errorf("gp.Name() = %q, want sanitized URL", gp.Name())
		}
		if gp.Type() != GitHub {
			t.Errorf("gp.Type() = %q, want %q", gp.Type(), GitHub)
		}
	})

	t.Run("GitHubProvider_Parse", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = rw.Write([]byte(content))
		}))
		defer server.Close()

		gp := GitHubProvider{
			PathOrURL:   "owner/repo",
			URLResolver: func(string) []string { return []string{server.URL + "/go.mod"} },
		}
		ps, err := gp.Parse(context.Background(), server.Client())
		if err != nil {
			t.Fatalf("gp.Parse failed: %v", err)
		}
		if ps.SourceType != GitHub || len(ps.Reqs) != 1 {
			t.Errorf("unexpected result: %+v", ps)
		}
	})
}

func BenchmarkSanitizeURL_Clean(b *testing.B) {
	url := "https://raw.githubusercontent.com/owner/repo/main/go.mod"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = SanitizeURL(url)
	}
}
