package utils

import (
	"strconv"
	"strings"

	"golang.org/x/mod/module"
)

// ParseModulePath splits a module path into its base path, current major version number, and separator string.
//
// Examples:
//
//	"github.com/user/gomodule/v2"  -> ("github.com/user/gomodule", 2, "/")
//	"gopkg.in/yaml.v2"             -> ("gopkg.in/yaml", 2, ".")
//	"github.com/google/uuid"       -> ("github.com/google/uuid", 1, "/")
func ParseModulePath(modPath string) (basePath string, major int, sep string) {
	prefix, pathMajor, ok := module.SplitPathVersion(modPath)
	if !ok || pathMajor == "" {
		return modPath, 1, "/"
	}
	sep = pathMajor[:1]
	n, err := strconv.Atoi(pathMajor[2:])
	if err != nil || (n < 2 && sep != ".") {
		return modPath, 1, "/"
	}
	return prefix, n, sep
}

// NextMajorPath builds the module path for the given major version, using the provided separator.
func NextMajorPath(basePath string, major int, sep string) string {
	if sep == "" {
		sep = "/"
	}
	if major <= 1 && sep == "/" {
		return basePath
	}
	return basePath + sep + "v" + strconv.Itoa(major)
}

// EscapePath applies Go module path escaping (uppercase letters become !lowercase).
func EscapePath(modPath string) (string, error) {
	return module.EscapePath(modPath)
}

// NormalizeSplitString splits a string by comma, space, tab, or newline, and trims whitespace.
func NormalizeSplitString(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}
