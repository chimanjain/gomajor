package sanitize

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	urlRegex                = regexp.MustCompile(`https?://[^\s"'<>\],]+`)
	schemelessUserPassRegex = regexp.MustCompile(`(^|[\s"'\(\[])([a-zA-Z0-9_.-]+:[^@\s/]+)@([a-zA-Z0-9_.-]+)`)
	standaloneTokenRegex    = regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{50,})\b`)
	authHeaderRegex         = regexp.MustCompile(`(?i)(bearer\s+|token\s+)[A-Za-z0-9_\-\.]{12,}`)
)

// URL strips credentials (usernames, passwords, or tokens), sensitive query
// parameters, and standalone authentication tokens from a URL or text containing URLs/errors.
func URL(raw string) string {
	if raw == "" {
		return raw
	}

	// Fast path: clean single URL with scheme and no spaces or surrounding punctuation
	if (strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")) &&
		!strings.ContainsAny(raw, " \t\n\r\"'[]<>,") &&
		!strings.Contains(raw, "@") && !strings.Contains(raw, "?") {
		return raw
	}

	// Fast path: if string contains neither an http scheme, user-info separator '@',
	// nor common token markers, none of the sanitizers can match.
	if !strings.Contains(raw, "http://") && !strings.Contains(raw, "https://") &&
		!strings.Contains(raw, "@") && !strings.Contains(raw, "gh") && !strings.Contains(raw, "github_pat") &&
		!strings.Contains(strings.ToLower(raw), "bearer") && !strings.Contains(strings.ToLower(raw), "token") {
		return raw
	}

	sanitized := raw
	// For general text (error messages, compound strings), find and sanitize all embedded URLs
	if strings.Contains(sanitized, "http://") || strings.Contains(sanitized, "https://") {
		sanitized = urlRegex.ReplaceAllStringFunc(sanitized, SingleURL)
	}

	// Also redact any schemeless credentials: user:pass@host
	if strings.Contains(sanitized, "@") {
		sanitized = schemelessUserPassRegex.ReplaceAllString(sanitized, "${1}redacted@${3}")
	}

	// Redact standalone GitHub tokens (e.g. ghp_..., github_pat_...) in text / error messages
	if strings.Contains(sanitized, "gh") || strings.Contains(sanitized, "github_pat") {
		sanitized = standaloneTokenRegex.ReplaceAllString(sanitized, "[REDACTED_TOKEN]")
	}

	// Redact Bearer or token auth headers in text / error messages
	lower := strings.ToLower(sanitized)
	if strings.Contains(lower, "bearer") || strings.Contains(lower, "token") {
		sanitized = authHeaderRegex.ReplaceAllString(sanitized, "${1}[REDACTED_TOKEN]")
	}

	return sanitized
}

// SingleURL sanitizes a single parsed HTTP/HTTPS URL.
func SingleURL(raw string) string {
	if !strings.Contains(raw, "@") && !strings.Contains(raw, "?") {
		return raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	modified := false

	if u.User != nil {
		u.User = url.User("redacted")
		modified = true
	}

	if u.RawQuery != "" {
		q := u.Query()
		for key := range q {
			if IsSensitiveParam(key) {
				q.Set(key, "REDACTED")
				modified = true
			}
		}
		if modified {
			u.RawQuery = q.Encode()
		}
	}

	if !modified {
		return raw
	}
	return u.String()
}

// IsSensitiveParam reports whether the query parameter key is a secret or token.
func IsSensitiveParam(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "key", "api_key", "apikey", "pat", "pass", "pwd", "bearer", "sig", "signature", "jwt", "session", "sessionid", "hash":
		return true
	}
	for _, sub := range []string{"token", "secret", "password", "auth", "credential", "private"} {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}
