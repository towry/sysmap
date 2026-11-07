package smap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTasksStatesCommentsAndColons(t *testing.T) {
	src := "\ufeff" + `# a comment
- [ ] checkout: expose the retry path
  - [~] ml.deploy_svc: roll out: carefully
- [x] ledger: post the reversal
`
	tasks, diags := ParseTasks(src)
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics = %+v", diags)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if got := tasks[0]; got.Node != "checkout" || got.Text != "expose the retry path" || got.State != TaskTodo || got.Line != 2 {
		t.Errorf("first task = %+v", got)
	}
	if got := tasks[1]; got.Node != "ml.deploy_svc" || got.Text != "roll out: carefully" || got.State != TaskDoing || got.Line != 3 {
		t.Errorf("dotted task = %+v", got)
	}
	if got := tasks[2]; got.State != TaskDone || got.Line != 4 {
		t.Errorf("done task = %+v", got)
	}
}

func TestParseTasksReportsBadLinesWithLineNumber(t *testing.T) {
	src := "- [ ] ok: fine\n- [?] bad: state\nnot a task\n- [ ] no colon\n- [ ] : empty\n- [ ] bad node: x\n"
	tasks, diags := ParseTasks(src)
	if len(tasks) != 1 || tasks[0].Node != "ok" {
		t.Fatalf("tasks = %+v", tasks)
	}
	want := []int{2, 3, 4, 5, 6}
	if len(diags) != len(want) {
		t.Fatalf("diagnostics = %+v", diags)
	}
	for i, line := range want {
		if diags[i].Line != line || diags[i].Level != "error" || !strings.Contains(diags[i].Msg, "cannot parse task line") {
			t.Errorf("diag %d = %+v, want line %d", i, diags[i], line)
		}
	}
}

func TestTasksCanTargetADottedMountedNode(t *testing.T) {
	root, _, _ := shopFiles(t)
	m := loadClean(t, root)
	tasks := []Task{{Node: "ml.deploy", State: TaskDone}}
	got := m.TaskProgress(tasks)
	if p := got["ml.deploy"]; p != (TaskProgress{Done: 1, Total: 1}) {
		t.Fatalf("mounted node progress = %+v", p)
	}
	if p := got["ml"]; p != (TaskProgress{Done: 1, Total: 1}) {
		t.Fatalf("mount node roll-up = %+v", p)
	}
}

func TestTaskProgressRollsUpAndCountsEachTaskOnce(t *testing.T) {
	m := mustParse(t, shop)
	tasks := []Task{
		{Node: "checkout", State: TaskDone},
		{Node: "checkout", State: TaskDoing},
		{Node: "orders", State: TaskDone},
		{Node: "ledger", State: TaskDone},
		{Node: "payment", State: TaskTodo},
		{Node: "invoice", State: TaskTodo},
		{Node: "missing", State: TaskDone},
	}
	got := m.TaskProgress(tasks)
	for id, want := range map[string]TaskProgress{
		"checkout": {Done: 1, Doing: 1, Total: 2},
		"orders":   {Done: 2, Doing: 1, Total: 3},
		"platform": {Done: 1, Total: 1},
		"ledger":   {Done: 1, Total: 1},
		"billing":  {Total: 2},
		"payment":  {Total: 1},
		"invoice":  {Total: 1},
	} {
		if got[id] != want {
			t.Errorf("%s progress = %+v, want %+v", id, got[id], want)
		}
	}
	if p, ok := got["cart"]; ok {
		t.Errorf("node with no tasks has progress %+v", p)
	}
	if p, ok := got["missing"]; ok {
		t.Errorf("unknown tasks must not create progress: %+v", p)
	}
}

func TestCheckTasksWarnsForUnknownAndUnplannedTaskGaps(t *testing.T) {
	m := mustParse(t, "+ biz new \"New\"\n~ biz old \"Old\"\nbiz with \"With\"\n")
	tasks := []Task{
		{Node: "new", State: TaskTodo, Line: 1},
		{Node: "with", State: TaskDone, Line: 2},
		{Node: "missing", State: TaskTodo, Line: 9},
	}
	diags := m.CheckTasks(tasks, "tasks.md")
	if len(diags) != 2 {
		t.Fatalf("task diagnostics = %+v", diags)
	}
	var unknown, planned bool
	for _, d := range diags {
		if d.Level != "warn" {
			t.Errorf("task diagnostic is not a warning: %+v", d)
		}
		if d.File == "tasks.md" && d.Line == 9 && strings.Contains(d.Msg, `unknown node "missing"`) {
			unknown = true
		}
		if strings.Contains(d.Msg, `planned node "old" has no task`) {
			planned = true
		}
	}
	if !unknown || !planned {
		t.Fatalf("missing expected warnings: %+v", diags)
	}
}

func TestJSONWithTasksCarriesOwnTasksAndRolledUpProgress(t *testing.T) {
	m := mustParse(t, shop)
	overlay := &TaskOverlay{
		Source: "- [x] checkout: expose the retry path\n- [~] checkout: record the state\n- [x] ledger: post the reversal\n",
		Tasks: []Task{
			{Node: "checkout", Text: "expose the retry path", State: TaskDone},
			{Node: "checkout", Text: "record the state", State: TaskDoing},
			{Node: "ledger", Text: "post the reversal", State: TaskDone},
		},
	}
	var v viewData
	if err := json.Unmarshal(m.JSONWithTasks("src", overlay, nil), &v); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]jsonNode{}
	for _, n := range v.Nodes {
		nodes[n.ID] = n
	}
	if p := nodes["checkout"].Progress; p == nil || *p != (jsonProgress{Done: 1, Doing: 1, Total: 2}) {
		t.Errorf("checkout progress = %+v", p)
	}
	if ts := nodes["checkout"].Tasks; len(ts) != 2 || ts[0].Text != "expose the retry path" || ts[1].State != "doing" {
		t.Errorf("checkout own tasks = %+v", ts)
	}
	if p := nodes["orders"].Progress; p == nil || *p != (jsonProgress{Done: 1, Doing: 1, Total: 2}) {
		t.Errorf("orders roll-up = %+v", p)
	}
	if ts := nodes["orders"].Tasks; len(ts) != 0 {
		t.Errorf("orders must not own a descendant task: %+v", ts)
	}
	if p := nodes["cart"].Progress; p != nil {
		t.Errorf("cart has no tasks but progress = %+v", p)
	}
}

func TestTaskSourceChangesTheVersion(t *testing.T) {
	m := mustParse(t, shop)
	version := func(src string) string {
		var v struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(m.JSONWithTasks("src", &TaskOverlay{Source: src}, nil), &v); err != nil {
			t.Fatal(err)
		}
		return v.Version
	}
	if before, after := version("- [ ] checkout: todo"), version("- [x] checkout: todo"); before == after {
		t.Fatalf("version %q did not change with the task source", before)
	}
}

func TestViewerWithoutTasksKeepsTheExactBaseline(t *testing.T) {
	m := mustParse(t, shop)
	page := m.HTML("src", nil, false)
	if len(page) != 155390 || fmt.Sprintf("%x", sha256.Sum256(page)) != "54477475cca5f5beaf01db57388b1bca3b8e95d233adb995364cfbb2f14fbb74" {
		t.Fatalf("no-tasks page changed: len=%d sha=%x", len(page), sha256.Sum256(page))
	}
	js := m.JSON("src", nil)
	if len(js) != 2712 || fmt.Sprintf("%x", sha256.Sum256(js)) != "599bce334e3fb3da80a779ca02f345c73804a05d03c1a116d84e2c293d488dcf" {
		t.Fatalf("no-tasks JSON changed: len=%d sha=%x", len(js), sha256.Sum256(js))
	}
}

func TestLoadTasksCarriesTheFileAndLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] a: fine\nnope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, tasks, diags, err := LoadTasks(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "nope") || len(tasks) != 1 || len(diags) != 1 || diags[0].File != path || diags[0].Line != 2 {
		t.Fatalf("load tasks = %q %+v %+v", src, tasks, diags)
	}
	if _, _, _, err := LoadTasks(filepath.Join(dir, "missing.md")); err == nil {
		t.Fatal("missing tasks file must be an error")
	}
}
