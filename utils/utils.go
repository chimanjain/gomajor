package utils

import (
	"context"
	"fmt"
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

func isValidGoEnvKey(key string) bool {
	if len(key) == 0 {
		return false
	}
	for _, r := range key {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func execCommandGoEnv(key string) (string, error) {
	if !isValidGoEnvKey(key) {
		return "", fmt.Errorf("invalid go env key: %q", key)
	}
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
	goEnvCache.Clear()
}

// NormalizeSplitString splits a string by comma, space, tab, or newline, and
// trims whitespace from each resulting element.
func NormalizeSplitString(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
}
