package smap

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Check validates references and applies the one boundary rule: a shared (core)
// node must not depend on a business node.
func (m *Model) Check() []Diag {
	var diags []Diag
	add := func(line int, level, format string, args ...any) {
		diags = append(diags, Diag{Line: line, Level: level, Msg: fmt.Sprintf(format, args...)})
	}

	for _, e := range m.Edges {
		m.checkEdge(e, add)
	}

	flows := map[string]int{}
	for _, f := range m.Flows {
		if first, dup := flows[f.ID]; dup {
			add(f.Line, "error", "duplicate flow id %q (first on line %d)", f.ID, first)
		}
		flows[f.ID] = f.Line
		if len(f.Steps) == 0 {
			add(f.Line, "warn", "flow %q has no steps", f.ID)
		}
		for _, s := range f.Steps {
			bad := false
			for _, id := range []string{s.From, s.To} {
				if m.Nodes[id] == nil {
					add(s.Line, "error", "flow %q step refers to unknown node %q", f.ID, id)
					bad = true
				}
			}
			if !bad && !m.hasDependency(s.From, s.To) {
				add(s.Line, "warn", "flow %q step %s -> %s has no matching edge; add the edge so the map shows it", f.ID, s.From, s.To)
			}
		}
	}
	return diags
}

// checkEdge applies the edge rules. Unknown ids are errors; the two boundary rules are warnings.
func (m *Model) checkEdge(e Edge, add func(line int, level, format string, args ...any)) {
	from, to := m.Nodes[e.From], m.Nodes[e.To]
	if from == nil || to == nil {
		for _, id := range []string{e.From, e.To} {
			if m.Nodes[id] == nil {
				hint := ""
				if strings.Contains(id, ".") {
					hint = " (ids inside a mounted file are not visible here; declare an `ext` stub in the mounted file and bind it)"
				}
				add(e.Line, "error", "edge refers to unknown node %q%s", id, hint)
			}
		}
		return
	}
	switch {
	case e.From == e.To:
		add(e.Line, "error", "edge from %q to itself", e.From)
	case m.InSubtree(e.To, e.From) || m.InSubtree(e.From, e.To):
		add(e.Line, "warn", "edge between %q and %q, which contain one another; draw it between siblings", e.From, e.To)
	case from.Kind == Core && to.Kind == Biz:
		add(e.Line, "warn", "shared node %q depends on business node %q; shared nodes should not know business nodes", e.From, e.To)
	}
}

// CheckRefs verifies every `@ path[:line]` reference. A path is resolved from the nearest
// ancestor of the declaring file that holds .git, or from the file's own directory when
// there is none. A line, when given for a regular file, must not exceed its line count.
func (m *Model) CheckRefs() []Diag {
	var diags []Diag
	add := func(file string, line int, format string, args ...any) {
		diags = append(diags, Diag{File: file, Line: line, Level: "error", Msg: fmt.Sprintf(format, args...)})
	}
	for _, id := range m.Order {
		n := m.Nodes[id]
		root := repoRoot(n.File)
		for i, ref := range n.Refs {
			line := n.Line
			if i < len(n.RefLines) {
				line = n.RefLines[i]
			}
			path, num := splitRef(ref)
			full := filepath.Join(root, filepath.FromSlash(path))
			info, err := os.Stat(full)
			if err != nil {
				add(n.File, line, "reference %q: %s does not exist", ref, path)
				continue
			}
			if num == 0 || !info.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				add(n.File, line, "reference %q: cannot read %s: %v", ref, path, err)
				continue
			}
			lines := bytes.Count(data, []byte("\n"))
			if len(data) > 0 && data[len(data)-1] != '\n' {
				lines++
			}
			if num > lines {
				add(n.File, line, "reference %q: %s has only %d lines", ref, path, lines)
			}
		}
	}
	return diags
}

// splitRef separates `path:line`; a colon not followed by a number stays part of the path.
func splitRef(ref string) (string, int) {
	i := strings.LastIndex(ref, ":")
	if i <= 0 {
		return ref, 0
	}
	n, err := strconv.Atoi(ref[i+1:])
	if err != nil || n < 1 {
		return ref, 0
	}
	return ref[:i], n
}

// repoRoot is the nearest ancestor of file's directory that contains .git, else that
// directory itself. The walk uses an absolute path, so the result does not depend on cwd.
func repoRoot(file string) string {
	own := filepath.Dir(file)
	dir, err := filepath.Abs(own)
	if err != nil {
		return own
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return own
		}
		dir = parent
	}
}

// hasDependency reports an edge from anywhere in from's subtree to anywhere in to's subtree.
func (m *Model) hasDependency(from, to string) bool {
	for _, e := range m.Edges {
		if m.InSubtree(e.From, from) && m.InSubtree(e.To, to) {
			return true
		}
	}
	return false
}
