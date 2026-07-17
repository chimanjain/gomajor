// Package utils provides shared string-manipulation utilities.
// Module-path parsing (ParseModulePath, NextMajorPath) lives in the
// internal/modpath package.
package utils

import "strings"

// NormalizeSplitString splits a string by comma, space, tab, or newline, and
// trims whitespace from each resulting element.
func NormalizeSplitString(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}
