// Package goenv provides utilities for reading and caching Go environment variables.
package goenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	envCache  sync.Map
	execGoEnv = execCommandGoEnv
)

// Get returns the effective value of a Go environment variable.
// Process environment variables (via os.Getenv) take precedence. If empty,
// it falls back to querying 'go env <key>' and caches the result for future lookups.
func Get(key string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	if cached, ok := envCache.Load(key); ok {
		return cached.(string)
	}

	val, err := execGoEnv(key)
	if err != nil {
		val = ""
	}

	envCache.Store(key, val)
	return val
}

// ClearCache clears the cached go env lookups.
func ClearCache() {
	envCache.Clear()
}

func execCommandGoEnv(key string) (string, error) {
	if !isValidGoEnvKey(key) {
		return "", os.ErrInvalid
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "env", key)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func isValidGoEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
