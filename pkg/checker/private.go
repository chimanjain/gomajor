package checker

import (
	"cmp"
	"sync/atomic"

	"github.com/chimanjain/gomajor/internal/goenv"
	"golang.org/x/mod/module"
)

type privateModuleMatcher struct {
	cache atomic.Pointer[string]
}

func (m *privateModuleMatcher) isPrivate(modPath string) bool {
	curr := m.cache.Load()
	if curr == nil {
		globs := cmp.Or(goenv.Get("GONOPROXY"), goenv.Get("GOPRIVATE"))
		m.cache.Store(&globs)
		curr = &globs
	}
	return *curr != "" && module.MatchPrefixPatterns(*curr, modPath)
}
