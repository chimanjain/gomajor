package utils

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	goEnvCache sync.Map
	execGoEnv  = execCommandGoEnv
)

func execCommandGoEnv(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "env", key)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetGoEnv returns the effective value of a Go environment variable.
// Process environment variables (via os.Getenv) take precedence. If empty,
// it falls back to querying 'go env <key>' and caches the result for future lookups.
func GetGoEnv(key string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	if cached, ok := goEnvCache.Load(key); ok {
		return cached.(string)
	}

	val, err := execGoEnv(key)
	if err != nil {
		val = ""
	}

	goEnvCache.Store(key, val)
	return val
}

// ClearGoEnvCache clears the cached go env lookups.
func ClearGoEnvCache() {
	goEnvCache.Range(func(key, _ any) bool {
		goEnvCache.Delete(key)
		return true
	})
}

// NormalizeSplitString splits a string by comma, space, tab, or newline, and
// trims whitespace from each resulting element.
func NormalizeSplitString(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}
