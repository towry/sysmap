package smap

import (
	"fmt"
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

// hasDependency reports an edge from anywhere in from's subtree to anywhere in to's subtree.
func (m *Model) hasDependency(from, to string) bool {
	for _, e := range m.Edges {
		if m.InSubtree(e.From, from) && m.InSubtree(e.To, to) {
			return true
		}
	}
	return false
}
