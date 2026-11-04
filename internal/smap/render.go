package smap

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"html"
	"strings"
)

//go:embed template.html
var template string

//go:embed vendor/dagre.min.js
var dagreJS string

//go:embed vendor/dagre.LICENSE
var dagreLicense string

// panzoom.min.js keeps its own copyright header, which stripComments leaves alone.
//
//go:embed vendor/panzoom.min.js
var panzoomJS string

// pele.global.js renders Mermaid diagrams. It is embedded only when the model has one.
//
//go:embed vendor/pele.global.js
var peleJS string

//go:embed vendor/pele.LICENSE
var peleLicense string

type jsonDiagram struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type jsonNode struct {
	ID       string        `json:"id"`
	Kind     Kind          `json:"kind"`
	Title    string        `json:"title"`
	Desc     string        `json:"desc"`
	Detail   []string      `json:"detail"`
	Refs     []string      `json:"refs"`
	Diagrams []jsonDiagram `json:"diagrams"`
	Parent   string        `json:"parent"`
	Children []string      `json:"children"`
	Mark     string        `json:"mark"`
	FanIn    int           `json:"fanin"`
	File     string        `json:"file"`
	Area     string        `json:"area"`
}

type jsonEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Async bool   `json:"async"`
	Label string `json:"label"`
}

type jsonStep struct {
	From string `json:"from"`
	To   string `json:"to"`
	Note string `json:"note"`
}

type jsonFlow struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	Desc  string     `json:"desc"`
	Steps []jsonStep `json:"steps"`
}

type viewData struct {
	Title   string     `json:"title"`
	Desc    string     `json:"desc"`
	Version string     `json:"version"`
	Nodes   []jsonNode `json:"nodes"`
	Edges   []jsonEdge `json:"edges"`
	Flows   []jsonFlow `json:"flows"`
	Impact  Impact     `json:"impact"`
	Diags   []Diag     `json:"diags"`
}

// JSON is the model as the page and `sysmap json` consume it. The model must have
// passed Check without errors.
func (m *Model) JSON(source string, diags []Diag) []byte {
	sum := sha256.Sum256([]byte(source))
	v := viewData{Title: m.Title, Desc: m.Desc, Version: hex.EncodeToString(sum[:6]),
		Nodes: []jsonNode{}, Edges: []jsonEdge{}, Flows: []jsonFlow{}, Impact: m.Impact(), Diags: diags}
	if v.Diags == nil {
		v.Diags = []Diag{}
	}
	for _, id := range m.Order {
		n := m.Nodes[id]
		v.Nodes = append(v.Nodes, jsonNode{ID: id, Kind: n.Kind, Title: n.Title, Desc: n.Desc,
			Detail: orEmpty(n.Detail), Refs: orEmpty(n.Refs), Diagrams: jsonDiagrams(n.Diagrams),
			Parent: n.Parent, Children: orEmpty(n.Children),
			Mark: n.Mark, FanIn: len(m.FanIn(id)), File: n.File, Area: m.Area(id)})
	}
	for _, e := range m.Edges {
		v.Edges = append(v.Edges, jsonEdge{From: e.From, To: e.To, Async: e.Async, Label: e.Label})
	}
	for _, f := range m.Flows {
		jf := jsonFlow{ID: f.ID, Title: f.Title, Desc: f.Desc, Steps: []jsonStep{}}
		for _, s := range f.Steps {
			jf.Steps = append(jf.Steps, jsonStep{From: s.From, To: s.To, Note: s.Note})
		}
		v.Flows = append(v.Flows, jf)
	}
	// Marshal escapes <, > and & so the data can sit inside a <script> tag. The types are plain
	// strings, numbers and slices, so an error here is a bug in this file, not bad input.
	out, err := json.Marshal(v)
	if err != nil {
		panic("sysmap: cannot encode the model: " + err.Error())
	}
	return out
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// HTML builds the single-file viewer. With live set, the page polls /data.json.
func (m *Model) HTML(source string, diags []Diag, live bool) []byte {
	title := m.Title
	if title == "" {
		title = "system map"
	}
	liveAttr := ""
	if live {
		liveAttr = ` data-live="1"`
	}
	// The renderer is most of the page, so a map without a diagram does not carry it. The
	// vendored banner is stripped and the licence is inlined without URL schemes: a built page
	// must not contain anything that looks like a network request.
	peleScript := ""
	if m.hasDiagrams() {
		notice := strings.NewReplacer("https://", "", "http://", "").Replace(peleLicense)
		peleScript = "<script>\n/*\nPele 1.13.0, MIT License\n" + notice + "*/\n" + stripComments(peleJS) + "\n</script>"
	}
	// One pass, so text inserted for one placeholder is never scanned for another.
	return []byte(strings.NewReplacer(
		"__TITLE__", html.EscapeString(title),
		"__LIVE__", liveAttr,
		"__PANZOOM__", stripComments(panzoomJS),
		"__DAGRE__", "/*\n@dagrejs/dagre\n"+dagreLicense+"*/\n"+stripComments(dagreJS),
		"__PELE_SCRIPT__", peleScript,
		"__DATA__", string(m.JSON(source, diags)),
	).Replace(template))
}

// hasDiagrams reports whether any node loaded a Mermaid file.
func (m *Model) hasDiagrams() bool {
	for _, id := range m.Order {
		if len(m.Nodes[id].Diagrams) > 0 {
			return true
		}
	}
	return false
}

func jsonDiagrams(in []Diagram) []jsonDiagram {
	out := make([]jsonDiagram, 0, len(in))
	for _, d := range in {
		out = append(out, jsonDiagram{Path: d.Path, Text: d.Text})
	}
	return out
}

// stripComments drops the licence banner and the trailing source-map lines of a vendored bundle.
func stripComments(js string) string {
	var keep []string
	for _, line := range strings.Split(js, "\n") {
		if !strings.HasPrefix(line, "//#") && !strings.HasPrefix(line, "/*!") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}
