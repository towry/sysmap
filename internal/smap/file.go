package smap

import (
	"fmt"
	"os"
	"path/filepath"
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

// Load reads a map and every file it mounts. It returns the combined source (what a version
// hash should cover), the composed model and every diagnostic, each with its file.
func Load(path string) (string, *Model, []Diag, error) {
	l := &loader{src: map[string]string{}}
	m, err := l.read(path)
	if err != nil {
		return "", nil, nil, err
	}
	abs, _ := filepath.Abs(path)
	l.expand(m, []string{abs})
	// Cycles are decided on the composed edges, after every bind has been applied.
	l.diags = append(l.diags, m.checkCycles()...)
	return l.source(m), m, l.finish(m), nil
}

// source is the root file alone for a single-file map, otherwise every file in order. The
// diagram text is part of it so a changed .mmd changes the version hash the page polls.
func (l *loader) source(m *Model) string {
	var b strings.Builder
	if len(m.Files) == 1 {
		b.WriteString(l.src[m.Files[0]])
	} else {
		for _, f := range m.Files {
			fmt.Fprintf(&b, "# file: %s\n%s\n", f, l.src[f])
		}
	}
	for _, d := range l.diagrams {
		fmt.Fprintf(&b, "\n# diagram: %s\n%s\n", d.path, d.text)
	}
	return b.String()
}

// finish drops repeated diagnostics, orders them by file then line, and leaves the file
// name off a single-file map, where it would only repeat the path the caller already has.
func (l *loader) finish(m *Model) []Diag {
	order := map[string]int{}
	for i, f := range m.Files {
		order[f] = i
	}
	seen := map[Diag]bool{}
	var out []Diag
	for _, d := range l.diags {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].File] != order[out[j].File] {
			return order[out[i].File] < order[out[j].File]
		}
		return out[i].Line < out[j].Line
	})
	if l.single(m) {
		for i := range out {
			out[i].File = ""
		}
	}
	return out
}

// single reports a map with one file and no mount declarations. A mount that failed still
// makes the map multi-file for diagnostics, even though only the root file was read.
func (l *loader) single(m *Model) bool {
	if len(m.Files) != 1 {
		return false
	}
	for _, n := range m.Nodes {
		if n.Mount != "" {
			return false
		}
	}
	return true
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

// AcceptFiles drops the planned markers in every file of a loaded map and writes each
// one back. It returns the number of markers removed.
func AcceptFiles(files []string) (int, error) {
	total := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return total, err
		}
		next, n := Accept(string(data))
		if err := os.WriteFile(file, []byte(next), 0o644); err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
