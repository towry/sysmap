package smap

import (
	"fmt"
	"regexp"
	"strings"
)

type Diag struct {
	Line  int    `json:"line"`
	Level string `json:"level"` // "error" | "warn"
	Msg   string `json:"msg"`
}

const idPat = `([A-Za-z0-9_.-]+)`

var (
	sysRe  = regexp.MustCompile(`^system(?:\s+"([^"]*)")?(?:\s*:\s*(.*))?$`)
	flowRe = regexp.MustCompile(`^flow\s+` + idPat + `(?:\s+"([^"]*)")?(?:\s*:\s*(.*))?$`)
	nodeRe = regexp.MustCompile(`^(?:([+~])\s+)?(core|biz|ext)\s+` + idPat + `(?:\s+"([^"]*)")?(?:\s*:\s*(.*))?$`)
	edgeRe = regexp.MustCompile(`^` + idPat + `\s+(->|~>)\s+` + idPat + `(?:\s*:\s*(.*))?$`)
)

type frame struct {
	n     *Node
	level int
}

// Parse reads the sysmap language. It reports every syntax problem with its line
// and still returns the model built from the lines that were valid.
func Parse(src string) (*Model, []Diag) {
	src = strings.TrimPrefix(src, "\ufeff") // a BOM from some editors
	m := &Model{Nodes: map[string]*Node{}}
	var diags []Diag
	fail := func(line int, format string, args ...any) {
		diags = append(diags, Diag{Line: line, Level: "error", Msg: fmt.Sprintf(format, args...)})
	}

	var stack []frame
	var last *Node
	flow := -1 // index into m.Flows of the open flow

	for i, raw := range strings.Split(src, "\n") {
		ln := i + 1
		raw = strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(raw)
		if t == "" || t[0] == '#' {
			continue
		}
		lead := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
		if strings.Contains(lead, "\t") {
			fail(ln, "indent with 2 spaces, not tabs")
			continue
		}
		if len(lead)%2 != 0 {
			fail(ln, "indent must be a multiple of 2 spaces")
			continue
		}
		level := len(lead) / 2
		if level == 0 {
			flow = -1
		}

		switch {
		case flow >= 0 && level > 0:
			// Only steps may sit inside a flow.
			e := edgeRe.FindStringSubmatch(t)
			if e == nil {
				fail(ln, "inside a flow only steps like `a -> b : note` are allowed")
				continue
			}
			m.Flows[flow].Steps = append(m.Flows[flow].Steps, Step{From: e[1], To: e[3], Note: e[4], Line: ln})

		case t[0] == '|':
			if last == nil {
				fail(ln, "`|` detail line must follow a node")
				continue
			}
			last.Detail = append(last.Detail, strings.TrimPrefix(strings.TrimPrefix(t, "|"), " "))

		case t[0] == '@':
			if last == nil {
				fail(ln, "`@` reference line must follow a node")
				continue
			}
			if ref := strings.TrimSpace(t[1:]); ref != "" {
				last.Refs = append(last.Refs, ref)
			}

		case sysRe.MatchString(t):
			s := sysRe.FindStringSubmatch(t)
			if level != 0 || m.Title != "" {
				fail(ln, "`system` appears once, at the top level")
				continue
			}
			m.Title, m.Desc = s[1], s[2]
			if m.Title == "" {
				m.Title = "system"
			}

		case flowRe.MatchString(t):
			f := flowRe.FindStringSubmatch(t)
			if level != 0 {
				fail(ln, "`flow` must be at the top level")
				continue
			}
			title := f[2]
			if title == "" {
				title = f[1]
			}
			m.Flows = append(m.Flows, Flow{ID: f[1], Title: title, Desc: f[3], Line: ln})
			flow = len(m.Flows) - 1
			stack, last = nil, nil

		case nodeRe.MatchString(t):
			p := nodeRe.FindStringSubmatch(t)
			n := &Node{Mark: p[1], Kind: Kind(p[2]), ID: p[3], Title: p[4], Desc: p[5], Line: ln}
			if n.Title == "" {
				n.Title = n.ID
			}
			if prev, dup := m.Nodes[n.ID]; dup {
				fail(ln, "duplicate id %q (first declared on line %d)", n.ID, prev.Line)
				continue
			}
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			if level > 0 {
				if len(stack) == 0 || stack[len(stack)-1].level != level-1 {
					fail(ln, "indent jumps a level or has no parent node")
					continue
				}
				n.Parent = stack[len(stack)-1].n.ID
				m.Nodes[n.Parent].Children = append(m.Nodes[n.Parent].Children, n.ID)
			} else {
				m.Roots = append(m.Roots, n.ID)
			}
			m.Nodes[n.ID] = n
			m.Order = append(m.Order, n.ID)
			stack = append(stack, frame{n, level})
			last = n

		case edgeRe.MatchString(t):
			e := edgeRe.FindStringSubmatch(t)
			m.Edges = append(m.Edges, Edge{From: e[1], To: e[3], Async: e[2] == "~>", Label: e[4], Line: ln})
			last = nil // a following | or @ line has no node to describe

		default:
			if first := strings.Fields(t)[0]; first == "core" || first == "biz" || first == "ext" || first == "flow" || first == "system" || first == "+" || first == "~" {
				fail(ln, "malformed %s line: ids use letters, digits and . _ - only, and a title cannot contain a double quote", first)
				continue
			}
			fail(ln, "cannot parse this line; expected `core|biz|ext <id> \"Title\" : desc`, `a -> b : label`, `flow <id>`, `| detail` or `@ path`")
		}
	}
	return m, diags
}
