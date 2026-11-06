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

// Diagram-count and task-progress badges are injected only when the page can show them,
// so a plain map keeps its exact current output.
//
//go:embed diagbadge.css
var diagBadgeCSS string

//go:embed diagbadge.js
var diagBadgeJS string

//go:embed taskbadge.css
var taskBadgeCSS string

//go:embed taskbadge.js
var taskBadgeJS string

type jsonDiagram struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

type jsonTask struct {
	Text  string `json:"text"`
	State string `json:"state"`
}

type jsonProgress struct {
	Done  int `json:"done"`
	Doing int `json:"doing"`
	Total int `json:"total"`
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
	Progress *jsonProgress `json:"progress,omitempty"`
	Tasks    []jsonTask    `json:"tasks,omitempty"`
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
	Title          string        `json:"title"`
	Desc           string        `json:"desc"`
	Version        string        `json:"version"`
	Nodes          []jsonNode    `json:"nodes"`
	Edges          []jsonEdge    `json:"edges"`
	Flows          []jsonFlow    `json:"flows"`
	Impact         Impact        `json:"impact"`
	Diags          []Diag        `json:"diags"`
	ProgramVersion string        `json:"programVersion"`
	FileVersions   []FileVersion `json:"fileVersions"`
}

// JSON is the model as the page and `sysmap json` consume it. The model must have
// passed Check without errors.
func (m *Model) JSON(source string, diags []Diag) []byte {
	return m.JSONWithTasks(source, nil, diags)
}

// JSONWithTasks is JSON plus the view-time task overlay. A nil overlay keeps the output
// byte-for-byte unchanged.
func (m *Model) JSONWithTasks(source string, tasks *TaskOverlay, diags []Diag) []byte {
	versionSource := source
	if tasks != nil {
		versionSource = source + "\n# tasks\n" + tasks.Source
	}
	sum := sha256.Sum256([]byte(versionSource))
	v := viewData{Title: m.Title, Desc: m.Desc, Version: hex.EncodeToString(sum[:6]),
		Nodes: []jsonNode{}, Edges: []jsonEdge{}, Flows: []jsonFlow{}, Impact: m.Impact(), Diags: diags, ProgramVersion: ProgramVersion, FileVersions: append([]FileVersion{}, m.FileVersions...)}
	for i := range v.FileVersions {
		v.FileVersions[i].Advice = v.FileVersions[i].guidance()
	}
	if v.Diags == nil {
		v.Diags = []Diag{}
	}
	var progress map[string]TaskProgress
	var own map[string][]Task
	if tasks != nil {
		progress = m.TaskProgress(tasks.Tasks)
		own = m.OwnTasks(tasks.Tasks)
	}
	for _, id := range m.Order {
		n := m.Nodes[id]
		jn := jsonNode{ID: id, Kind: n.Kind, Title: n.Title, Desc: n.Desc,
			Detail: orEmpty(n.Detail), Refs: orEmpty(n.Refs), Diagrams: jsonDiagrams(n.Diagrams),
			Parent: n.Parent, Children: orEmpty(n.Children),
			Mark: n.Mark, FanIn: len(m.FanIn(id)), File: n.File, Area: m.Area(id)}
		if p, ok := progress[id]; ok && p.Total > 0 {
			jn.Progress = &jsonProgress{Done: p.Done, Doing: p.Doing, Total: p.Total}
		}
		if ts := own[id]; len(ts) > 0 {
			jn.Tasks = jsonTasks(ts)
		}
		v.Nodes = append(v.Nodes, jn)
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
	return m.HTMLWithTasks(source, nil, diags, live)
}

// HTMLWithTasks builds the viewer with the task overlay. A nil overlay also omits the badge
// assets, so a page with no diagrams and no tasks keeps its exact previous bytes.
func (m *Model) HTMLWithTasks(source string, tasks *TaskOverlay, diags []Diag, live bool) []byte {
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
	badges := ""
	diagBadge, taskBadge, taskDetail := "", "", ""
	if m.hasDiagrams() {
		badges += "<style>" + diagBadgeCSS + "</style><script>" + diagBadgeJS + "</script>"
		diagBadge = "${diagBadge(n)}"
	}
	if tasks != nil {
		badges += "<style>" + taskBadgeCSS + "</style><script>" + taskBadgeJS + "</script>"
		taskBadge, taskDetail = "${taskBadge(n)}", "${taskDetail(n)}"
	}
	// One pass, so text inserted for one placeholder is never scanned for another.
	return []byte(strings.NewReplacer(
		"__TITLE__", html.EscapeString(title),
		"__LIVE__", liveAttr,
		"__PANZOOM__", stripComments(panzoomJS),
		"__DAGRE__", "/*\n@dagrejs/dagre\n"+dagreLicense+"*/\n"+stripComments(dagreJS),
		"__PELE_SCRIPT__", peleScript,
		"__BADGE_ASSETS__", badges,
		"__DIAG_BADGE__", diagBadge,
		"__TASK_BADGE__", taskBadge,
		"__TASK_DETAIL__", taskDetail,
		"__DATA__", string(m.JSONWithTasks(source, tasks, diags)),
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

func jsonTasks(in []Task) []jsonTask {
	out := make([]jsonTask, 0, len(in))
	for _, t := range in {
		out = append(out, jsonTask{Text: t.Text, State: string(t.State)})
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
