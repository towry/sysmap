package smap

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func (m *Model) mark(n *Node) string {
	if n.Mark == "" {
		return ""
	}
	return n.Mark + " "
}

// Outline prints the tree under root ("" for the whole system) down to depth levels.
// This is the first thing an agent reads: kind, id, title, one line of purpose, how
// many nodes depend on it, and how many children are hidden below the depth limit.
func (m *Model) Outline(root string, depth int) (string, error) {
	var b strings.Builder
	counts := map[Kind]int{}
	planned := 0
	for _, n := range m.Nodes {
		counts[n.Kind]++
		if n.Mark != "" {
			planned++
		}
	}
	if root != "" {
		root = m.resolve(root)
		if m.Nodes[root] == nil {
			return "", fmt.Errorf("unknown node %q", root)
		}
	}
	if root == "" {
		title := m.Title
		if title == "" {
			title = "system"
		}
		fmt.Fprintf(&b, "%s", title)
		if m.Desc != "" {
			fmt.Fprintf(&b, " — %s", m.Desc)
		}
		fmt.Fprintf(&b, "\ncore %d · biz %d · ext %d · edges %d · flows %d", counts[Core], counts[Biz], counts[Ext], len(m.Edges), len(m.Flows))
		if planned > 0 {
			fmt.Fprintf(&b, " · planned %d (run `sysmap impact`)", planned)
		}
		b.WriteString("\n")
	}
	var walk func(id string, level int)
	walk = func(id string, level int) {
		n := m.Nodes[id]
		fmt.Fprintf(&b, "%s%s[%s] %s %q", strings.Repeat("  ", level), m.mark(n), n.Kind, n.ID, n.Title)
		if in := len(m.FanIn(id)); in > 0 {
			fmt.Fprintf(&b, " ←%d", in)
		}
		if n.Desc != "" {
			fmt.Fprintf(&b, " — %s", clip(n.Desc, 70))
		}
		// A mount is a whole subsystem: always show its size and where it comes from.
		if n.Mount != "" {
			fmt.Fprintf(&b, " (+%d below, %d planned) [mount %s]", len(m.Subtree(id))-1, m.plannedBelow(id), n.Mount)
		} else if level+1 >= depth && len(n.Children) > 0 {
			fmt.Fprintf(&b, " (+%d below)", len(m.Subtree(id))-1)
		}
		b.WriteString("\n")
		if level+1 < depth {
			for _, c := range n.Children {
				walk(c, level+1)
			}
		}
	}
	if root != "" {
		walk(root, 0)
		m.writeFlowList(&b, root)
		return b.String(), nil
	}
	for _, r := range m.Roots {
		walk(r, 0)
	}
	m.writeFlowList(&b, "")
	return b.String(), nil
}

// plannedBelow counts the marked descendants of id.
func (m *Model) plannedBelow(id string) int {
	n := 0
	for _, c := range m.Subtree(id)[1:] {
		if m.Nodes[c].Mark != "" {
			n++
		}
	}
	return n
}

// writeFlowList prints the flows declared in scope's file ("" for the root file). The root
// view also shows how many flows each mount brings in, since it does not list their ids.
func (m *Model) writeFlowList(b *strings.Builder, scope string) {
	var lines []string
	for _, f := range m.Flows {
		if f.Scope == scope {
			lines = append(lines, fmt.Sprintf("  %s %q (%d steps)", f.ID, f.Title, len(f.Steps)))
		}
	}
	if scope == "" {
		rootFile := ""
		if len(m.Files) > 0 {
			rootFile = m.Files[0]
		}
		for _, id := range m.Order {
			n := m.Nodes[id]
			if n.Mount == "" || (rootFile != "" && n.File != rootFile) {
				continue
			}
			count := 0
			for _, f := range m.Flows {
				if m.InSubtree(f.Scope, id) {
					count++
				}
			}
			lines = append(lines, fmt.Sprintf("  %s: %d flows", id, count))
		}
	}
	if len(lines) > 0 {
		b.WriteString("flows:\n")
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
	}
}

// where formats a declaration location as file:line, or just the line when there is no file.
func where(file string, line int) string {
	if file == "" {
		return fmt.Sprintf("%d", line)
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// Show prints everything about one node: the zoomed-in view. An id that a bind removed
// still resolves, and so does the local stub name the mounted file used.
func (m *Model) Show(id string) (string, error) {
	id = m.resolve(id)
	n := m.Nodes[id]
	if n == nil {
		return "", fmt.Errorf("unknown node %q", id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s[%s] %s %q", m.mark(n), n.Kind, n.ID, n.Title)
	if len(m.Files) > 1 {
		fmt.Fprintf(&b, " (%s)", where(n.File, n.Line))
	}
	b.WriteString("\n")
	if path := m.Ancestors(id); len(path) > 0 {
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}
		fmt.Fprintf(&b, "inside: %s\n", strings.Join(path, " > "))
	}
	if n.Desc != "" {
		fmt.Fprintf(&b, "%s\n", n.Desc)
	}
	for _, d := range n.Detail {
		fmt.Fprintf(&b, "  | %s\n", d)
	}
	for _, r := range n.Refs {
		fmt.Fprintf(&b, "  @ %s\n", r)
	}
	if len(n.Binds) > 0 {
		b.WriteString("binds:\n")
		for _, bind := range n.Binds {
			fmt.Fprintf(&b, "  %s = %s\n", bind.Stub, bind.Target)
		}
	}
	if len(n.Children) > 0 {
		b.WriteString("children:\n")
		for _, c := range n.Children {
			cn := m.Nodes[c]
			fmt.Fprintf(&b, "  %s[%s] %s %q\n", m.mark(cn), cn.Kind, cn.ID, cn.Title)
		}
	}
	m.writeEdges(&b, "depends on", false, id, func(e Edge) (string, bool) {
		return e.To, m.InSubtree(e.From, id) && !m.InSubtree(e.To, id)
	})
	m.writeEdges(&b, "used by", true, id, func(e Edge) (string, bool) {
		return e.From, m.InSubtree(e.To, id) && !m.InSubtree(e.From, id)
	})
	var inFlows []string
	for _, f := range m.Flows {
		for _, s := range f.Steps {
			if m.InSubtree(s.From, id) || m.InSubtree(s.To, id) {
				inFlows = append(inFlows, f.ID)
				break
			}
		}
	}
	if len(inFlows) > 0 {
		fmt.Fprintf(&b, "flows: %s\n", strings.Join(inFlows, ", "))
	}
	return b.String(), nil
}

// Find lists every node whose id, title, description or detail line contains query,
// ignoring case: the qualified id, kind, title and where it is declared, one per line.
// The first matching description, detail or title follows, clipped around the match.
func (m *Model) Find(query string) string {
	q := strings.ToLower(query)
	var b strings.Builder
	for _, id := range m.Order {
		n := m.Nodes[id]
		match := strings.Contains(strings.ToLower(n.ID), q) ||
			strings.Contains(strings.ToLower(n.Title), q) ||
			strings.Contains(strings.ToLower(n.Desc), q)
		for _, d := range n.Detail {
			match = match || strings.Contains(strings.ToLower(d), q)
		}
		if !match {
			continue
		}
		fmt.Fprintf(&b, "%s [%s] %q %s\n", n.ID, n.Kind, n.Title, where(n.File, n.Line))
		snippet := ""
		switch {
		case strings.Contains(strings.ToLower(n.Desc), q):
			snippet = "  desc: " + clipAround(n.Desc, query, 100)
		default:
			for _, d := range n.Detail {
				if strings.Contains(strings.ToLower(d), q) {
					snippet = "  | " + clipAround(d, query, 100)
					break
				}
			}
			if snippet == "" && strings.Contains(strings.ToLower(n.Title), q) {
				snippet = "  title: " + clipAround(n.Title, query, 100)
			}
		}
		if snippet != "" {
			b.WriteString(snippet + "\n")
		}
	}
	return b.String()
}

// clipAround cuts s to at most limit runes, keeping the first match of query visible.
func clipAround(s, query string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	at := 0
	if i := strings.Index(strings.ToLower(s), strings.ToLower(query)); i >= 0 {
		at = utf8.RuneCountInString(s[:i])
	}
	start := at - limit/3
	if start < 0 {
		start = 0
	}
	if start+limit > len(r) {
		start = len(r) - limit
	}
	end := start + limit
	prefix := start > 0 && start < at
	suffix := end < len(r) && end > at
	if prefix {
		start++
	}
	if suffix {
		end--
	}
	if start > at {
		start = at
	}
	if end <= at {
		end = at + 1
	}
	out := string(r[start:end])
	if prefix {
		out = "…" + out
	}
	if suffix {
		out += "…"
	}
	return out
}

// insideEnd names the end of e that sits inside id when it is a descendant, not id itself.
func (m *Model) insideEnd(e Edge, id string) string {
	for _, end := range []string{e.From, e.To} {
		if end != id && m.InSubtree(end, id) {
			return end
		}
	}
	return ""
}

func (m *Model) writeEdges(b *strings.Builder, label string, reverse bool, id string, pick func(Edge) (string, bool)) {
	var lines []string
	for _, e := range m.Edges {
		other, ok := pick(e)
		if !ok {
			continue
		}
		arrow := map[[2]bool]string{{false, false}: "->", {false, true}: "~>", {true, false}: "<-", {true, true}: "<~"}[[2]bool{reverse, e.Async}]
		line := fmt.Sprintf("  %s %s [%s]", arrow, other, m.Nodes[other].Kind)
		if len(m.Files) > 1 {
			line += " (" + where(e.File, e.Line) + ")"
		}
		if inner := m.insideEnd(e, id); inner != "" {
			line += " (via " + inner + ")"
		}
		if e.Label != "" {
			line += " : " + e.Label
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		fmt.Fprintf(b, "%s:\n%s\n", label, strings.Join(lines, "\n"))
	}
}
