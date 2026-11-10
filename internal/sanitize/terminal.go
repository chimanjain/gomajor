// Package sanitize provides utilities for stripping ANSI/BiDi terminal injection
// characters, redacting credentials from URLs, and masking sensitive tokens.
package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// IsCleanASCII reports whether s contains only printable ASCII characters
// without ANSI escape sequences, ASCII control characters, or multibyte Unicode characters.
func IsCleanASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b >= 0x7F {
			return false
		}
	}
	return true
}

// SkipEscapeSeq skips ANSI escape sequences (CSI, OSC, DCS, 2-byte) starting at s[i].
func SkipEscapeSeq(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI sequence: ESC [ ... [0x40-0x7E]
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7E {
				return j + 1
			}
			if s[j] < 0x20 || s[j] == 0x7F || s[j] == '\x1b' {
				return j
			}
		}
	case ']', 'P', '^', '_': // OSC / DCS / PM / APC sequence: ESC ] ... (ST / BEL)
		for j := i + 2; j < len(s); j++ {
			if s[j] == '\x07' {
				return j + 1
			}
			if s[j] == '\x1b' {
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2
				}
				return j
			}
		}
	default:
		if s[i+1] >= 0x20 && s[i+1] <= 0x7E {
			return i + 2
		}
	}
	return i + 1
}

// SanitizeAndVisualLen strips ANSI escape sequences, ASCII control characters,
// and Unicode bidirectional / invisible formatting characters, while simultaneously
// computing the display width in terminal columns in a single pass.
func SanitizeAndVisualLen(s string) (string, int) {
	if IsCleanASCII(s) {
		return s, len(s)
	}

	var b strings.Builder
	b.Grow(len(s))
	var count int

	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			i = SkipEscapeSeq(s, i)
			continue
		}

		if s[i] < utf8.RuneSelf {
			bVal := s[i]
			i++
			if bVal < 0x20 || bVal == 0x7F {
				continue
			}
			b.WriteByte(bVal)
			count++
			continue
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		i += size

		// Strip BiDi control characters (CVE-2021-42574 Trojan Source defense)
		if unicode.Is(unicode.Bidi_Control, r) {
			continue
		}
		// Strip invisible formatting characters and zero-width spaces
		if unicode.Is(unicode.Cf, r) || r == '\u200B' || r == '\uFEFF' {
			continue
		}

		b.WriteRune(r)
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			count += 2
		default:
			count++
		}
	}
	return b.String(), count
}

// SanitizeTerminalString strips ANSI escape sequences, ASCII control characters,
// and Unicode bidirectional / invisible formatting characters to prevent terminal injection.
func SanitizeTerminalString(s string) string {
	sanitized, _ := SanitizeAndVisualLen(s)
	return sanitized
}

// VisualLen returns the display width of a string in terminal columns.
// ANSI escape sequences, BiDi controls, and zero-width characters occupy 0 columns,
// and East-Asian Wide / Fullwidth characters are counted as 2 columns each.
func VisualLen(s string) int {
	_, vlen := SanitizeAndVisualLen(s)
	return vlen
}
