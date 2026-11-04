package smap

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// DefaultFiles are tried in order when no file is named.
var DefaultFiles = []string{"system.smap", ".sysmap/system.smap"}

// Resolve picks the model file: the argument, then $SYSMAP_FILE, then the defaults.
func Resolve(arg string) (string, error) {
	if arg != "" {
		return arg, nil
	}
	if env := os.Getenv("SYSMAP_FILE"); env != "" {
		return env, nil
	}
	for _, f := range DefaultFiles {
		if _, err := os.Stat(f); err == nil {
			return f, nil
		}
	}
	return "", fmt.Errorf("no model file: pass a path, set SYSMAP_FILE, or create %s", strings.Join(DefaultFiles, " or "))
}

// Load reads and parses a file, returning the source, the model and every diagnostic.
func Load(path string) (string, *Model, []Diag, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, err
	}
	m, diags := Parse(string(data))
	diags = append(diags, m.Check()...)
	sort.SliceStable(diags, func(i, j int) bool { return diags[i].Line < diags[j].Line })
	return string(data), m, diags, nil
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(diags []Diag) bool {
	for _, d := range diags {
		if d.Level == "error" {
			return true
		}
	}
	return false
}

var markerRe = regexp.MustCompile(`(?m)^(\s*)[+~]\s+(core|biz|ext)\b`)

// Accept drops the planned markers once the change has shipped.
func Accept(source string) (string, int) {
	n := len(markerRe.FindAllString(source, -1))
	return markerRe.ReplaceAllString(source, "$1$2"), n
}
