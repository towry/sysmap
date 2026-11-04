package smap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxMountDepth = 8

// loader reads a root file and every file it mounts, and turns them into one model.
//
// A mounted file is a complete map of its own. Its ids stay local in the file, and in the
// composed model each one is prefixed with the id of the node that mounts it (ml.deploy_svc).
// Everything outside a mounted file is an `ext` stub inside it, which the mounting file joins
// to a real node with `bind`, so the file can still be checked on its own.
type loader struct {
	src   map[string]string // file path -> contents
	diags []Diag
}

func (l *loader) add(file string, line int, level, format string, args ...any) {
	l.diags = append(l.diags, Diag{File: file, Line: line, Level: level, Msg: fmt.Sprintf(format, args...)})
}

// read parses one file, tags everything in it with the path and runs the per-file checks.
func (l *loader) read(path string) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l.src[path] = string(data)
	m, diags := Parse(string(data))
	for _, d := range append(diags, m.Check()...) {
		d.File = path
		l.diags = append(l.diags, d)
	}
	for _, n := range m.Nodes {
		n.File = path
	}
	for i := range m.Edges {
		m.Edges[i].File = path
	}
	for i := range m.Flows {
		m.Flows[i].File = path
	}
	m.Files = []string{path}
	return m, nil
}

// expand mounts every file m asks for, then resolves the binds. All mounts come first, so a
// bind may name a node in a sibling mount that appears later in the file.
func (l *loader) expand(m *Model, stack []string) {
	ids := append([]string(nil), m.Order...)
	mounted := map[string]bool{}
	for _, id := range ids {
		n := m.Nodes[id]
		if n.Mount != "" {
			mounted[id] = l.mount(m, n, stack)
		} else if len(n.Binds) > 0 {
			l.add(n.File, n.Binds[0].Line, "error", "bind %q must sit under a node that mounts a file", n.Binds[0].Stub)
		}
	}
	rewired := map[int]bool{}
	for _, id := range ids {
		if n := m.Nodes[id]; mounted[id] {
			l.bind(m, n, rewired)
		}
	}
	// A bind can turn an ext stub into a core node, which changes what the edge rules say.
	for i := range m.Edges {
		if rewired[i] {
			m.checkEdge(m.Edges[i], func(line int, level, format string, args ...any) {
				l.add(m.Edges[i].File, line, level, format, args...)
			})
		}
	}
}

func (l *loader) mount(m *Model, n *Node, stack []string) bool {
	if filepath.IsAbs(n.Mount) || strings.Contains(n.Mount, "://") {
		l.add(n.File, n.MountLine, "error", "mount path %q must be relative to the file that mounts it", n.Mount)
		return false
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(n.File), n.Mount))
	abs, _ := filepath.Abs(path)
	for _, s := range stack {
		if s == abs {
			l.add(n.File, n.MountLine, "error", "mount cycle: %s is already being mounted above %q", path, n.ID)
			return false
		}
	}
	if len(stack) > maxMountDepth {
		l.add(n.File, n.MountLine, "error", "mounts nest deeper than %d mounts", maxMountDepth)
		return false
	}
	child, err := l.read(path)
	if err != nil {
		l.add(n.File, n.MountLine, "error", "cannot read mounted file %s: %v", path, err)
		return false
	}
	l.expand(child, append(stack, abs))

	prefix := n.ID + "."
	for _, cid := range child.Order {
		cn := child.Nodes[cid]
		cn.ID = prefix + cid
		if cn.Parent == "" {
			cn.Parent = n.ID
			n.Children = append(n.Children, cn.ID)
		} else {
			cn.Parent = prefix + cn.Parent
		}
		for i, c := range cn.Children {
			cn.Children[i] = prefix + c
		}
		m.Nodes[cn.ID] = cn
		m.Order = append(m.Order, cn.ID)
	}
	for _, e := range child.Edges {
		e.From, e.To = prefix+e.From, prefix+e.To
		m.Edges = append(m.Edges, e)
	}
	for _, f := range child.Flows {
		f.ID = prefix + f.ID
		for i := range f.Steps {
			f.Steps[i].From, f.Steps[i].To = prefix+f.Steps[i].From, prefix+f.Steps[i].To
		}
		if f.Scope == "" {
			f.Scope = n.ID
		} else {
			f.Scope = prefix + f.Scope
		}
		m.Flows = append(m.Flows, f)
	}
	for k, v := range child.Alias {
		m.Alias[prefix+k] = prefix + v
	}
	for _, f := range child.Files {
		known := false
		for _, g := range m.Files {
			known = known || g == f
		}
		if !known {
			m.Files = append(m.Files, f)
		}
	}
	if n.Desc == "" {
		n.Desc = child.Desc
	}
	return true
}

// resolve follows aliases left by earlier binds, so a bind may name a node that was itself a stub.
func (m *Model) resolve(id string) string {
	for i := 0; i < 16; i++ {
		next, ok := m.Alias[id]
		if !ok {
			break
		}
		id = next
	}
	return id
}

func (l *loader) bind(m *Model, n *Node, rewired map[int]bool) {
	for _, b := range n.Binds {
		stubID := n.ID + "." + b.Stub
		fail := func(format string, args ...any) { l.add(n.File, b.Line, "error", format, args...) }
		if to, done := m.Alias[stubID]; done {
			fail("%q is already bound to %q", b.Stub, to)
			continue
		}
		stub := m.Nodes[stubID]
		switch {
		case stub == nil:
			fail("bind: the mounted file %s has no node %q", n.Mount, b.Stub)
			continue
		case stub.Kind != Ext:
			fail("bind: %q is a %s node; only an ext stub can be bound", b.Stub, stub.Kind)
			continue
		case len(stub.Children) > 0:
			fail("bind: %q has children; bind a leaf stub", b.Stub)
			continue
		}
		target := m.resolve(b.Target)
		switch {
		case m.Nodes[target] == nil:
			fail("bind: unknown target %q", b.Target)
			continue
		case m.InSubtree(target, n.ID):
			fail("bind: target %q is inside the file it is bound from", b.Target)
			continue
		}

		for i := range m.Edges {
			e := &m.Edges[i]
			if e.From == stubID || e.To == stubID {
				if e.From == stubID {
					e.From = target
				}
				if e.To == stubID {
					e.To = target
				}
				rewired[i] = true
			}
		}
		for fi := range m.Flows {
			for si := range m.Flows[fi].Steps {
				s := &m.Flows[fi].Steps[si]
				if s.From == stubID {
					s.From = target
				}
				if s.To == stubID {
					s.To = target
				}
			}
		}
		m.Alias[stubID] = target
		m.remove(stubID)
		// An ext container that only held bound stubs has nothing left to show.
		for p := stub.Parent; p != n.ID; {
			pn := m.Nodes[p]
			if pn.Kind != Ext || len(pn.Children) > 0 || pn.Mount != "" || m.namesNode(p) {
				break
			}
			m.remove(p)
			p = pn.Parent
		}
	}
}

// remove takes a node out of the tree and the declaration order.
func (m *Model) remove(id string) {
	n := m.Nodes[id]
	if p := m.Nodes[n.Parent]; p != nil {
		p.Children = without(p.Children, id)
	}
	m.Roots = without(m.Roots, id)
	m.Order = without(m.Order, id)
	delete(m.Nodes, id)
}

func without(list []string, id string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// namesNode reports whether any edge or flow step names id.
func (m *Model) namesNode(id string) bool {
	for _, e := range m.Edges {
		if e.From == id || e.To == id {
			return true
		}
	}
	for _, f := range m.Flows {
		for _, s := range f.Steps {
			if s.From == id || s.To == id {
				return true
			}
		}
	}
	return false
}
