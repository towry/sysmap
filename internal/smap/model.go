// Package smap parses the sysmap text language and answers questions about it.
package smap

import "sort"

type Kind string

const (
	Core Kind = "core" // shared boundary used across business nodes
	Biz  Kind = "biz"  // business-specific node
	Ext  Kind = "ext"  // system outside the one being mapped
)

// Bind joins an `ext` stub inside a mounted file to a node the mounting file can see.
type Bind struct {
	Stub, Target string
	Line         int
}

// Diagram is a Mermaid file loaded from an `@` ref whose path ends in .mmd or .mermaid.
type Diagram struct {
	Path string // the ref as it was written
	Text string
}

type Node struct {
	ID        string
	Kind      Kind
	Title     string
	Desc      string
	Mark      string // "" | "+" planned new | "~" planned change
	Detail    []string
	Refs      []string
	RefLines  []int // the line each ref was written on, parallel to Refs
	Diagrams  []Diagram
	Parent    string
	Children  []string
	Line      int
	File      string // the .smap file that declares the node; empty for a single-file map
	Mount     string // path of a .smap file whose top-level nodes become this node's children
	MountLine int
	Binds     []Bind
}

type Edge struct {
	From, To string
	Async    bool
	Label    string
	Line     int
	File     string
}

type Step struct {
	From, To, Note string
	Line           int
}

type Flow struct {
	ID, Title, Desc string
	Steps           []Step
	Line            int
	File            string
	Scope           string // the mount node whose file declares the flow; "" for the root file
}

type Model struct {
	Title, Desc  string
	Nodes        map[string]*Node
	Order        []string // declaration order
	Roots        []string
	Edges        []Edge
	Flows        []Flow
	Files        []string // every .smap file in the map, the root first
	FileVersions []FileVersion
	Alias        map[string]string // an id that a bind removed -> the node it was bound to
}

// Ancestors returns the parents of id, nearest first.
func (m *Model) Ancestors(id string) []string {
	var out []string
	for n := m.Nodes[id]; n != nil && n.Parent != ""; n = m.Nodes[n.Parent] {
		out = append(out, n.Parent)
	}
	return out
}

// InSubtree reports whether id is root or lives under root.
func (m *Model) InSubtree(id, root string) bool {
	if id == root {
		return true
	}
	for _, a := range m.Ancestors(id) {
		if a == root {
			return true
		}
	}
	return false
}

// Subtree lists id and all its descendants.
func (m *Model) Subtree(id string) []string {
	out := []string{id}
	for _, c := range m.Nodes[id].Children {
		out = append(out, m.Subtree(c)...)
	}
	return out
}

// RootOf returns the top-level ancestor of id within its own file (id itself when it is
// top-level there). A mounted file's top-level nodes are roots of their own areas.
func (m *Model) RootOf(id string) string {
	cur := id
	for {
		n := m.Nodes[cur]
		if n == nil || n.Parent == "" || m.Nodes[n.Parent].File != n.File {
			return cur
		}
		cur = n.Parent
	}
}

// Area is the container a node is drawn inside when areas are shown: its file-local
// top-level ancestor, or the mount node for a mounted file's top-level nodes. Top-level
// nodes of the root file have none.
func (m *Model) Area(id string) string {
	if m.Nodes[id] == nil {
		return ""
	}
	if r := m.RootOf(id); r != id {
		return r
	}
	return m.Nodes[id].Parent
}

// FanIn lists the distinct nodes outside id's subtree that have an edge into it. A source
// inside another source (a container and its child) counts once, as the container.
func (m *Model) FanIn(id string) []string {
	seen := map[string]bool{}
	for _, e := range m.Edges {
		if m.InSubtree(e.To, id) && !m.InSubtree(e.From, id) {
			seen[e.From] = true
		}
	}
	for src := range seen {
		for _, a := range m.Ancestors(src) {
			if seen[a] {
				delete(seen, src)
				break
			}
		}
	}
	return sortedKeys(seen)
}

// FanOut lists the distinct nodes outside id's subtree that id's subtree depends on.
func (m *Model) FanOut(id string) []string {
	seen := map[string]bool{}
	for _, e := range m.Edges {
		if m.InSubtree(e.From, id) && !m.InSubtree(e.To, id) {
			seen[e.To] = true
		}
	}
	return sortedKeys(seen)
}

// Dependents is the transitive set of nodes that break if id changes. An edge to a container
// counts as using each node inside it, so it reaches every changed descendant.
func (m *Model) Dependents(id string) []string {
	paths := m.DependentPaths(id)
	out := make([]string, 0, len(paths))
	for d := range paths {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// DependentPaths maps each transitive dependent of id to the shortest chain that reaches id:
// [checkout payment ledger] means checkout depends on payment, which depends on ledger.
// When an edge reaches a node through one of its ancestor containers (for example web -> orders
// reaching ~ checkout inside orders), that container sits in the chain between the caller and
// the child ([web orders checkout]). A new (+) node only takes edges into its own subtree and
// stops at callers already marked + or ~, which track their own dependents.
func (m *Model) DependentPaths(id string) map[string][]string {
	type hop struct {
		to, via string
	}
	isNew := m.Nodes[id] != nil && m.Nodes[id].Mark == "+"
	next := map[string]hop{} // dependent -> the node (and optional container) it was reached through
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range m.Edges {
			if m.InSubtree(e.From, cur) || m.InSubtree(e.From, id) || m.InSubtree(e.From, e.To) {
				continue
			}
			via := ""
			switch {
			case m.InSubtree(e.To, cur):
			case !(isNew && cur == id) && m.InSubtree(cur, e.To):
				via = e.To
			default:
				continue
			}
			if isNew && m.Nodes[e.From] != nil && m.Nodes[e.From].Mark != "" {
				continue
			}
			prev, seen := next[e.From]
			if !seen || (prev.to == cur && prev.via != "" && (via == "" || m.InSubtree(via, prev.via))) {
				next[e.From] = hop{to: cur, via: via}
				if !seen {
					queue = append(queue, e.From)
				}
			}
		}
	}
	paths := make(map[string][]string, len(next))
	for d := range next {
		chain := []string{d}
		for at := d; at != id; {
			h := next[at]
			if h.via != "" {
				chain = append(chain, h.via)
			}
			chain = append(chain, h.to)
			at = h.to
		}
		paths[d] = chain
	}
	return paths
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
