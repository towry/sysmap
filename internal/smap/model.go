// Package smap parses the sysmap text language and answers questions about it.
package smap

import (
	"sort"
	"strings"
)

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
	ID          string
	Kind        Kind
	Title       string
	Desc        string
	Mark        string // "" | "+" planned new | "~" planned change
	Detail      []string
	DetailLines []int // the line each detail was written on, parallel to Detail
	Refs        []string
	RefLines    []int // the line each ref was written on, parallel to Refs
	Diagrams    []Diagram
	Parent      string
	Children    []string
	Line        int
	File        string // the .smap file that declares the node; empty for a single-file map
	Mount       string // path of a .smap file whose top-level nodes become this node's children
	MountLine   int
	Binds       []Bind
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
// the child ([web orders checkout]). When an async write edge (writer ~> core_store) feeds a
// shared store that downstream readers depend on (reader -> core_store), the store sits in the
// chain as via ([reader core_store writer]). A new (+) node only takes edges into its own
// subtree and stops at callers already marked + or ~, which track their own dependents.
func (m *Model) DependentPaths(id string) map[string][]string {
	type hop struct {
		to, via string
	}
	isNew := m.Nodes[id] != nil && m.Nodes[id].Mark == "+"
	next := map[string]hop{} // dependent -> the node (and optional container/store) it was reached through
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range m.Edges {
			if m.InSubtree(e.From, cur) && e.Async && !(isNew && cur == id) && !m.InSubtree(e.To, cur) && !m.InSubtree(e.To, id) {
				if store := m.Nodes[e.To]; store != nil && store.Kind == Core {
					for _, re := range m.Edges {
						if re.Async || !m.InSubtree(re.To, store.ID) || m.InSubtree(re.From, store.ID) || m.InSubtree(re.From, cur) || m.InSubtree(re.From, id) {
							continue
						}
						if isNew && m.Nodes[re.From] != nil && m.Nodes[re.From].Mark != "" {
							continue
						}
						if _, seen := next[re.From]; !seen {
							next[re.From] = hop{to: cur, via: store.ID}
							queue = append(queue, re.From)
						}
					}
				}
			}
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

// StoreBranch describes one fed shared store and the upstream write chain that populates it.
type StoreBranch struct {
	Store   string   `json:"store"`
	Writers []string `json:"writers"`
	Chain   []string `json:"chain"`
	Scope   string   `json:"scope,omitempty"`
}

// StoreJoin describes a consumer node that reads two or more independently fed shared stores.
type StoreJoin struct {
	Consumer string        `json:"consumer"`
	Join     string        `json:"join,omitempty"`
	Stores   []StoreBranch `json:"stores"`
}

// DetailValue returns the trimmed text after the first detail line starting with prefix
// (such as "scope:" or "join:"), or "" when none is present.
func DetailValue(n *Node, prefix string) string {
	if n == nil {
		return ""
	}
	for _, d := range n.Detail {
		if strings.HasPrefix(d, prefix) {
			if v := strings.TrimSpace(d[len(prefix):]); v != "" {
				return v
			}
		}
	}
	return ""
}

// StoreWriters returns the sorted external nodes that asynchronously write (~>) into id's subtree.
func (m *Model) StoreWriters(id string) []string {
	n := m.Nodes[id]
	if n == nil || n.Kind != Core {
		return nil
	}
	seen := map[string]bool{}
	for _, e := range m.Edges {
		if e.Async && m.InSubtree(e.To, id) && !m.InSubtree(e.From, id) {
			seen[e.From] = true
		}
	}
	return sortedKeys(seen)
}

// WriteChain traces the deepest upstream data-production chain ending at storeID
// (for example [vendor catalog_sync catalog_store bar_pool bar_writer bar_store]).
func (m *Model) WriteChain(storeID string) []string {
	writers := m.StoreWriters(storeID)
	if len(writers) == 0 {
		return nil
	}
	var best []string
	for _, w := range writers {
		chain := []string{w, storeID}
		visited := map[string]bool{storeID: true, w: true}
		cur := w
		for step := 0; step < 6; step++ {
			next := m.upstreamProducer(cur, visited)
			if next == "" {
				break
			}
			visited[next] = true
			chain = append([]string{next}, chain...)
			if m.Nodes[next] != nil && m.Nodes[next].Kind == Ext {
				break
			}
			cur = next
		}
		if len(chain) > len(best) {
			best = chain
		}
	}
	return best
}

func (m *Model) upstreamProducer(id string, visited map[string]bool) string {
	// 1. Prefer another fed core store that id reads synchronously (e.g. bar_pool -> catalog_store).
	for _, e := range m.Edges {
		if !e.Async && e.From == id && !visited[e.To] && len(m.StoreWriters(e.To)) > 0 {
			return e.To
		}
	}
	// 2. If id is a fed core store, step to its first unvisited async writer (e.g. catalog_sync ~> catalog_store).
	for _, w := range m.StoreWriters(id) {
		if !visited[w] {
			return w
		}
	}
	// 3. Prefer a biz node that feeds or is called by id (e.g. bar_pool -> bar_writer)
	// when that biz node itself reads a fed core store.
	for _, e := range m.Edges {
		cand := ""
		if e.To == id && !visited[e.From] {
			cand = e.From
		} else if !e.Async && e.From == id && !visited[e.To] {
			cand = e.To
		}
		if cand != "" && m.Nodes[cand] != nil && m.Nodes[cand].Kind == Biz {
			for _, re := range m.Edges {
				if !re.Async && re.From == cand && len(m.StoreWriters(re.To)) > 0 {
					return cand
				}
			}
		}
	}
	// 4. Otherwise step to an external data source that id pulls from (e.g. writer -> vendor).
	for _, e := range m.Edges {
		if !e.Async && e.From == id && !visited[e.To] && m.Nodes[e.To] != nil && m.Nodes[e.To].Kind == Ext {
			return e.To
		}
	}
	return ""
}

// NodeStoreJoin returns the StoreJoin for consumer id when id synchronously reads two or more
// independently fed shared (core) stores that have no direct edge between them.
func (m *Model) NodeStoreJoin(id string) *StoreJoin {
	n := m.Nodes[id]
	if n == nil {
		return nil
	}
	seenStore := map[string]bool{}
	var stores []string
	for _, e := range m.Edges {
		if e.Async || e.From != id || seenStore[e.To] {
			continue
		}
		writers := m.StoreWriters(e.To)
		if len(writers) == 0 {
			continue
		}
		// Skip a store if id is itself one of its writers or sits on its upstream write chain.
		isFeeder := false
		for _, w := range writers {
			if w == id {
				isFeeder = true
				break
			}
		}
		for _, c := range m.WriteChain(e.To) {
			if c == id {
				isFeeder = true
				break
			}
		}
		if isFeeder {
			continue
		}
		seenStore[e.To] = true
		stores = append(stores, e.To)
	}
	if len(stores) < 2 {
		return nil
	}
	// Check that at least two stores have distinct writer sets and no direct edge between them.
	independent := false
	for i := 0; i < len(stores) && !independent; i++ {
		for j := i + 1; j < len(stores); j++ {
			a, b := stores[i], stores[j]
			if strings.Join(m.StoreWriters(a), ",") == strings.Join(m.StoreWriters(b), ",") {
				continue
			}
			if m.hasDependency(a, b) || m.hasDependency(b, a) {
				continue
			}
			independent = true
			break
		}
	}
	if !independent {
		return nil
	}
	var branches []StoreBranch
	for _, s := range stores {
		branches = append(branches, StoreBranch{
			Store:   s,
			Writers: m.StoreWriters(s),
			Chain:   m.WriteChain(s),
			Scope:   DetailValue(m.Nodes[s], "scope:"),
		})
	}
	return &StoreJoin{
		Consumer: id,
		Join:     DetailValue(n, "join:"),
		Stores:   branches,
	}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
