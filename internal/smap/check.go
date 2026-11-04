package smap

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Check validates references and applies the boundary rules: a shared (core) node
// must not depend on or contain business code, and dependencies must not form cycles.
func (m *Model) Check() []Diag {
	return append(m.checkLocal(), m.checkCycles()...)
}

// checkLocal validates everything that can be decided file by file. Load runs it for
// each file before composition, so diagnostics keep the ids written in that file.
func (m *Model) checkLocal() []Diag {
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
	diags = append(diags, m.checkContainment()...)
	return diags
}

// checkContainment warns when business code sits directly inside a shared node. A mounted
// file's top-level nodes are judged by their parent inside that file, not by the kind the
// mounting file gave the mount node, so a mount boundary is exempt.
func (m *Model) checkContainment() []Diag {
	var diags []Diag
	for _, id := range m.Order {
		n := m.Nodes[id]
		if n.Kind != Biz || n.Parent == "" {
			continue
		}
		parent := m.Nodes[n.Parent]
		if parent == nil || parent.Kind != Core || parent.File != n.File {
			continue
		}
		diags = append(diags, Diag{File: n.File, Line: n.Line, Level: "warn",
			Msg: fmt.Sprintf("business node %q sits inside shared node %q; shared nodes should not contain business code", n.ID, parent.ID)})
	}
	return diags
}

// checkCycles warns once per strongly connected group of nodes on the composed edges.
// Self-loops and container/descendant edges are already reported, so they are left out.
// A group is confirmed, and not warned about, when one of its nodes has a non-empty
// `| cycle: <reason>` detail line.
func (m *Model) checkCycles() []Diag {
	adj := make(map[string][]string, len(m.Nodes))
	for _, e := range m.Edges {
		if m.cycleEdge(e) {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}

	index := 0
	seen := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var groups [][]string
	var visit func(string)
	visit = func(id string) {
		seen[id], low[id] = index, index
		index++
		stack = append(stack, id)
		onStack[id] = true
		for _, to := range adj[id] {
			if _, ok := seen[to]; !ok {
				visit(to)
				if low[to] < low[id] {
					low[id] = low[to]
				}
			} else if onStack[to] && seen[to] < low[id] {
				low[id] = seen[to]
			}
		}
		if low[id] != seen[id] {
			return
		}
		var group []string
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			group = append(group, last)
			if last == id {
				break
			}
		}
		groups = append(groups, group)
	}
	for _, id := range m.Order {
		if _, ok := seen[id]; !ok {
			visit(id)
		}
	}

	var diags []Diag
	for _, group := range groups {
		if len(group) < 2 || m.cycleConfirmed(group) {
			continue
		}
		sort.Strings(group)
		e := m.cycleGroupEdge(group)
		diags = append(diags, Diag{File: e.File, Line: e.Line, Level: "warn",
			Msg: fmt.Sprintf("dependency cycle between %s; confirm it with a \"| cycle: <reason>\" detail line on one of them or break it", joinIDs(group))})
	}
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].File != diags[j].File {
			return diags[i].File < diags[j].File
		}
		return diags[i].Line < diags[j].Line
	})
	return diags
}

// cycleEdge reports whether e can take part in a cycle: both ends exist, it is not a
// self-loop, and it does not join a container with its own descendant.
func (m *Model) cycleEdge(e Edge) bool {
	if e.From == e.To || m.Nodes[e.From] == nil || m.Nodes[e.To] == nil {
		return false
	}
	return !m.InSubtree(e.To, e.From) && !m.InSubtree(e.From, e.To)
}

// cycleGroupEdge picks the first declared edge inside the group, for file:line.
func (m *Model) cycleGroupEdge(group []string) Edge {
	in := make(map[string]bool, len(group))
	for _, id := range group {
		in[id] = true
	}
	for _, e := range m.Edges {
		if in[e.From] && in[e.To] && m.cycleEdge(e) {
			return e
		}
	}
	return Edge{}
}

// cycleConfirmed reports a deliberate cycle: a detail line starting with `cycle:` and
// followed by non-empty text, on any node in the group.
func (m *Model) cycleConfirmed(group []string) bool {
	for _, id := range group {
		for _, detail := range m.Nodes[id].Detail {
			if strings.HasPrefix(detail, "cycle:") && strings.TrimSpace(strings.TrimPrefix(detail, "cycle:")) != "" {
				return true
			}
		}
	}
	return false
}

func joinIDs(ids []string) string {
	if len(ids) == 2 {
		return ids[0] + " and " + ids[1]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
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
