package smap

import (
	"fmt"
	"sort"
	"strings"
)

// PlanItem is what one planned node (+ new, ~ changed) touches.
type PlanItem struct {
	ID         string   `json:"id"`
	Mark       string   `json:"mark"`
	Kind       Kind     `json:"kind"`
	Title      string   `json:"title"`
	ReusesCore []string `json:"reusesCore"`
	DependsBiz []string `json:"dependsBiz"`
	DependsExt []string `json:"dependsExt"`
	Dependents []string `json:"dependents"`
	// Paths gives, for each existing dependent, the chain from it to this node, so a viewer can
	// say who a node is affected by and through which nodes: ["checkout","payment","ledger"].
	Paths map[string][]string `json:"paths"`
	Flows []string            `json:"flows"`
	Notes []Note              `json:"notes"`
}

// Note is a warning about a planned node. The viewer words it from Code; Text is for the CLI.
type Note struct {
	Code string `json:"code"` // "cross-area-biz" | "no-dependency"
	Node string `json:"node,omitempty"`
	Area string `json:"area,omitempty"`
}

func (n Note) Text() string {
	if n.Code == "cross-area-biz" {
		area := n.Area
		if area == "" {
			area = "the top level"
		}
		return fmt.Sprintf("depends on business node %q in another area (%s)", n.Node, area)
	}
	return "declares no dependency, so it is unclear which shared nodes it reuses"
}

// Impact summarises how far the planned nodes reach into the rest of the system.
type Impact struct {
	// Level is "none", "extension" (only new business nodes), "contained"
	// (changes business nodes or adds a shared node) or "wide" (changes a shared node).
	Level    string     `json:"level"`
	Verdict  string     `json:"verdict"`
	Summary  Summary    `json:"summary"`
	Items    []PlanItem `json:"items"`
	Affected []string   `json:"affected"` // existing nodes the change reaches: dependents of ~ nodes, and nodes wired to + nodes
}

// Summary groups the planned nodes by what they do, so a viewer can phrase the verdict
// in its own language instead of parsing Verdict.
type Summary struct {
	ChangedCore []string `json:"changedCore"`
	NewCore     []string `json:"newCore"`
	ChangedBiz  []string `json:"changedBiz"`
	ChangedExt  []string `json:"changedExt"`
	NewOther    []string `json:"newOther"`
	Wired       []string `json:"wired"`  // existing nodes that depend on a new (+) node, so they must change too
	Reused      int      `json:"reused"` // distinct shared nodes the new nodes depend on
}

func (m *Model) Impact() Impact {
	imp := Impact{Level: "none", Verdict: "No planned nodes. Mark a node with `+` (new) or `~` (changed) to see its impact.", Items: []PlanItem{}, Affected: []string{},
		Summary: Summary{ChangedCore: []string{}, NewCore: []string{}, ChangedBiz: []string{}, ChangedExt: []string{}, NewOther: []string{}, Wired: []string{}}}
	affected := map[string]bool{}
	wired := map[string]bool{}
	rank := map[string]int{"none": 0, "extension": 1, "contained": 2, "wide": 3}
	raise := func(level string) {
		if rank[level] > rank[imp.Level] {
			imp.Level = level
		}
	}

	for _, id := range m.Order {
		n := m.Nodes[id]
		if n.Mark == "" {
			continue
		}
		item := PlanItem{ID: id, Mark: n.Mark, Kind: n.Kind, Title: n.Title,
			ReusesCore: []string{}, DependsBiz: []string{}, DependsExt: []string{}, Dependents: []string{}, Paths: map[string][]string{}, Flows: []string{}, Notes: []Note{}}

		for _, to := range m.FanOut(id) {
			switch m.Nodes[to].Kind {
			case Core:
				item.ReusesCore = append(item.ReusesCore, to)
			case Biz:
				item.DependsBiz = append(item.DependsBiz, to)
				if area := m.Area(to); area != m.Area(id) {
					item.Notes = append(item.Notes, Note{Code: "cross-area-biz", Node: to, Area: area})
				}
			default:
				item.DependsExt = append(item.DependsExt, to)
			}
		}
		paths := m.DependentPaths(id)
		for _, d := range m.Dependents(id) {
			if m.Nodes[d].Mark == "" {
				item.Dependents = append(item.Dependents, d)
				item.Paths[d] = paths[d]
			}
		}
		for _, d := range item.Dependents {
			affected[d] = true
			if n.Mark == "+" {
				wired[d] = true // an existing node that points at a new one has to be edited
				raise("contained")
			}
		}
		touched := map[string]bool{id: true}
		for _, d := range item.Dependents {
			touched[d] = true
		}
		for _, f := range m.Flows {
			for _, s := range f.Steps {
				if m.touches(touched, s.From) || m.touches(touched, s.To) {
					item.Flows = append(item.Flows, f.ID)
					break
				}
			}
		}
		if n.Mark == "+" && len(item.ReusesCore)+len(item.DependsBiz)+len(item.DependsExt) == 0 {
			item.Notes = append(item.Notes, Note{Code: "no-dependency"})
		}

		switch {
		case n.Mark == "~" && n.Kind == Core:
			raise("wide")
		case n.Mark == "+" && n.Kind == Core, n.Mark == "~":
			raise("contained")
		default:
			raise("extension")
		}
		imp.Items = append(imp.Items, item)
	}
	imp.Affected = sortedKeys(affected)
	imp.Summary.Wired = sortedKeys(wired)
	summarize(&imp)
	imp.Verdict = verdict(imp)
	return imp
}

func (m *Model) touches(set map[string]bool, id string) bool {
	for k := range set {
		if m.InSubtree(id, k) {
			return true
		}
	}
	return false
}

// summarize fills imp.Summary from the plan items.
func summarize(imp *Impact) {
	reused := map[string]bool{}
	for _, it := range imp.Items {
		for _, c := range it.ReusesCore {
			reused[c] = true
		}
		switch {
		case it.Mark == "~" && it.Kind == Core:
			imp.Summary.ChangedCore = append(imp.Summary.ChangedCore, it.ID)
		case it.Mark == "+" && it.Kind == Core:
			imp.Summary.NewCore = append(imp.Summary.NewCore, it.ID)
		case it.Mark == "~" && it.Kind == Ext:
			imp.Summary.ChangedExt = append(imp.Summary.ChangedExt, it.ID)
		case it.Mark == "~":
			imp.Summary.ChangedBiz = append(imp.Summary.ChangedBiz, it.ID)
		default:
			imp.Summary.NewOther = append(imp.Summary.NewOther, it.ID)
		}
	}
	imp.Summary.Reused = len(reused)
}

func verdict(imp Impact) string {
	s := imp.Summary
	var parts []string
	if len(s.ChangedCore) > 0 {
		parts = append(parts, fmt.Sprintf("changes shared node(s) %s, reaching %d existing node(s)", strings.Join(s.ChangedCore, ", "), len(imp.Affected)))
	}
	if len(s.NewCore) > 0 {
		parts = append(parts, fmt.Sprintf("adds shared node(s) %s, a new common point", strings.Join(s.NewCore, ", ")))
	}
	if len(s.ChangedBiz) > 0 {
		parts = append(parts, fmt.Sprintf("changes business node(s) %s, reaching %d existing node(s)", strings.Join(s.ChangedBiz, ", "), len(imp.Affected)))
	}
	if len(s.ChangedExt) > 0 {
		parts = append(parts, fmt.Sprintf("changes external system(s) %s, reaching %d existing node(s)", strings.Join(s.ChangedExt, ", "), len(imp.Affected)))
	}
	if len(s.NewOther) > 0 {
		tail := ""
		if len(parts) == 0 && len(s.Wired) == 0 {
			tail = " and changes no existing node"
		}
		parts = append(parts, fmt.Sprintf("adds %d new node(s) reusing %d shared node(s)%s", len(s.NewOther), s.Reused, tail))
	}
	if len(s.Wired) > 0 {
		parts = append(parts, fmt.Sprintf("existing node(s) %s depend on the new node(s) and must change too", strings.Join(s.Wired, ", ")))
	}
	if len(parts) == 0 {
		return imp.Verdict
	}
	label := map[string]string{"extension": "EXTENSION", "contained": "CONTAINED", "wide": "WIDE"}[imp.Level]
	return label + ": " + strings.Join(parts, "; ")
}

// Text renders the impact as the short report agents read.
func (imp Impact) Text() string {
	var b strings.Builder
	fmt.Fprintln(&b, imp.Verdict)
	for _, it := range imp.Items {
		fmt.Fprintf(&b, "\n%s %s %q\n", it.Mark, it.ID, it.Title)
		list := func(label string, ids []string) {
			if len(ids) > 0 {
				fmt.Fprintf(&b, "  %s: %s\n", label, strings.Join(ids, ", "))
			}
		}
		list("reuses shared", it.ReusesCore)
		list("depends on business", it.DependsBiz)
		list("depends on external", it.DependsExt)
		sort.Strings(it.Flows)
		list("flows touched", it.Flows)
		if len(it.Dependents) > 0 {
			fmt.Fprintf(&b, "  existing dependents (%d):\n", len(it.Dependents))
			for _, d := range it.Dependents {
				if p := it.Paths[d]; len(p) > 2 {
					fmt.Fprintf(&b, "    %s (via %s)\n", d, strings.Join(p[1:len(p)-1], " > "))
				} else {
					fmt.Fprintf(&b, "    %s\n", d)
				}
			}
		}
		for _, n := range it.Notes {
			fmt.Fprintf(&b, "  note: %s\n", n.Text())
		}
	}
	return b.String()
}
