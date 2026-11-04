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
		if level+1 >= depth && len(n.Children) > 0 {
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
		if m.Nodes[root] == nil {
			return "", fmt.Errorf("unknown node %q", root)
		}
		walk(root, 0)
		return b.String(), nil
	}
	for _, r := range m.Roots {
		walk(r, 0)
	}
	if len(m.Flows) > 0 {
		b.WriteString("flows:\n")
		for _, f := range m.Flows {
			fmt.Fprintf(&b, "  %s %q (%d steps)\n", f.ID, f.Title, len(f.Steps))
		}
	}
	return b.String(), nil
}

// Show prints everything about one node: the zoomed-in view.
func (m *Model) Show(id string) (string, error) {
	n := m.Nodes[id]
	if n == nil {
		return "", fmt.Errorf("unknown node %q", id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s[%s] %s %q\n", m.mark(n), n.Kind, n.ID, n.Title)
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
