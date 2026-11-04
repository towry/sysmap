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

type jsonNode struct {
	ID       string   `json:"id"`
	Kind     Kind     `json:"kind"`
	Title    string   `json:"title"`
	Desc     string   `json:"desc"`
	Detail   []string `json:"detail"`
	Refs     []string `json:"refs"`
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
	Mark     string   `json:"mark"`
	FanIn    int      `json:"fanin"`
	File     string   `json:"file"`
	Area     string   `json:"area"`
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
			Detail: orEmpty(n.Detail), Refs: orEmpty(n.Refs), Parent: n.Parent, Children: orEmpty(n.Children),
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
	// One pass, so text inserted for one placeholder is never scanned for another.
	return []byte(strings.NewReplacer(
		"__TITLE__", html.EscapeString(title),
		"__LIVE__", liveAttr,
		"__PANZOOM__", stripComments(panzoomJS),
		"__DAGRE__", "/*\n@dagrejs/dagre\n"+dagreLicense+"*/\n"+stripComments(dagreJS),
		"__DATA__", string(m.JSON(source, diags)),
	).Replace(template))
}

// stripComments drops the trailing source-map and licence comment lines of the vendored bundle.
func stripComments(js string) string {
	var keep []string
	for _, line := range strings.Split(js, "\n") {
		if !strings.HasPrefix(line, "//#") && !strings.HasPrefix(line, "/*!") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}
