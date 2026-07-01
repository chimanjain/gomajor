package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/module"
)

// majorSuffixRe matches a trailing major-version segment in a module path.
// Handles both "/vN" (GitHub style) and ".vN" (gopkg.in style).
var majorSuffixRe = regexp.MustCompile(`((?:/|\.)(v\d+))$`)

// ParseModulePath splits a module path into its base path, current major version number, and separator string.
//
// Examples:
//
//	"github.com/user/gomodule/v2"  -> ("github.com/user/gomodule", 2, "/")
//	"gopkg.in/yaml.v2"             -> ("gopkg.in/yaml", 2, ".")
//	"github.com/google/uuid"       -> ("github.com/google/uuid", 1, "/")
func ParseModulePath(modPath string) (basePath string, major int, sep string) {
	loc := majorSuffixRe.FindStringSubmatchIndex(modPath)
	if loc == nil {
		return modPath, 1, "/"
	}
	sep = modPath[loc[2] : loc[2]+1]
	vStr := modPath[loc[4]:loc[5]] // e.g. "v2"
	n, err := strconv.Atoi(strings.TrimPrefix(vStr, "v"))
	// For dot separator (e.g. gopkg.in), major versions can be v0 or v1 (e.g. package.v1)
	isDot := sep == "."
	if err != nil || (n < 2 && !isDot) {
		return modPath, 1, "/"
	}
	// Remove the matched suffix from the path.
	base := modPath[:loc[2]]
	return base, n, sep
}

// NextMajorPath builds the module path for the given major version, using the provided separator.
func NextMajorPath(basePath string, major int, sep string) string {
	if sep == "" {
		sep = "/"
	}
	return fmt.Sprintf("%s%sv%d", basePath, sep, major)
}

// EscapePath applies Go module path escaping (uppercase letters become !lowercase).
func EscapePath(modPath string) (string, error) {
	return module.EscapePath(modPath)
}
