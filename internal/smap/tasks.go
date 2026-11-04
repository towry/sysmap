package smap

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// TaskState is the checkbox state of one task in a tasks file.
type TaskState string

const (
	TaskTodo  TaskState = "todo"
	TaskDoing TaskState = "doing"
	TaskDone  TaskState = "done"
)

// Task is one line of a Markdown checklist joined at view time, never stored in the .smap.
type Task struct {
	Node  string
	Text  string
	State TaskState
	Line  int
}

// TaskOverlay is a tasks file supplied with --tasks. Nil means the flag was not given.
type TaskOverlay struct {
	Source string
	Tasks  []Task
}

// TaskProgress is the roll-up for one node: its own tasks plus every descendant's tasks.
type TaskProgress struct {
	Done, Doing, Total int
}

var (
	taskLineRe = regexp.MustCompile(`^- \[([ ~x])\]\s+(.+)$`)
	taskNodeRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// ParseTasks reads the tasks checklist. Blank lines and lines starting with # are ignored;
// every other line must be `- [ ] node: text`, `- [~] node: text` or `- [x] node: text`.
func ParseTasks(src string) ([]Task, []Diag) {
	src = strings.TrimPrefix(src, "\ufeff") // a BOM from some editors
	var tasks []Task
	var diags []Diag
	for i, raw := range strings.Split(src, "\n") {
		line := i + 1
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		m := taskLineRe.FindStringSubmatch(text)
		if m == nil {
			diags = append(diags, badTaskLine(line))
			continue
		}
		body := m[2]
		at := strings.IndexByte(body, ':')
		if at <= 0 {
			diags = append(diags, badTaskLine(line))
			continue
		}
		node := strings.TrimSpace(body[:at])
		taskText := strings.TrimSpace(body[at+1:])
		if node == "" || taskText == "" || !taskNodeRe.MatchString(node) {
			diags = append(diags, badTaskLine(line))
			continue
		}
		state := TaskTodo
		switch m[1] {
		case "~":
			state = TaskDoing
		case "x":
			state = TaskDone
		}
		tasks = append(tasks, Task{Node: node, Text: taskText, State: state, Line: line})
	}
	return tasks, diags
}

func badTaskLine(line int) Diag {
	return Diag{Line: line, Level: "error", Msg: "cannot parse task line; expected `- [ ] node-id: text` ([~] doing, [x] done)"}
}

// LoadTasks reads and parses one tasks file. The returned source is what the version hash
// must cover; a read error is an error, while bad lines are diagnostics.
func LoadTasks(path string) (string, []Task, []Diag, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, err
	}
	src := string(data)
	tasks, diags := ParseTasks(src)
	for i := range diags {
		diags[i].File = path
	}
	return src, tasks, diags, nil
}

// OwnTasks groups the tasks attached directly to each node. Unknown ids are skipped here;
// CheckTasks reports them.
func (m *Model) OwnTasks(tasks []Task) map[string][]Task {
	out := map[string][]Task{}
	for _, t := range tasks {
		id := m.resolve(t.Node)
		if m.Nodes[id] != nil {
			out[id] = append(out[id], t)
		}
	}
	return out
}

// TaskProgress rolls every task up through its node's ancestors. Each task is counted once,
// on its own node and on every container above it; a node with no tasks has no entry.
func (m *Model) TaskProgress(tasks []Task) map[string]TaskProgress {
	out := map[string]TaskProgress{}
	for _, t := range tasks {
		id := m.resolve(t.Node)
		if m.Nodes[id] == nil {
			continue
		}
		for cur := id; cur != ""; cur = m.Nodes[cur].Parent {
			p := out[cur]
			p.Total++
			switch t.State {
			case TaskDone:
				p.Done++
			case TaskDoing:
				p.Doing++
			}
			out[cur] = p
		}
	}
	return out
}

// CheckTasks validates the tasks overlay: every task must name a node, and every planned
// node must carry at least one task of its own. Problems are warnings, not errors.
func (m *Model) CheckTasks(tasks []Task, file string) []Diag {
	own := m.OwnTasks(tasks)
	var diags []Diag
	for _, t := range tasks {
		if m.Nodes[m.resolve(t.Node)] == nil {
			diags = append(diags, Diag{File: file, Line: t.Line, Level: "warn",
				Msg: fmt.Sprintf("task names unknown node %q", t.Node)})
		}
	}
	for _, id := range m.Order {
		n := m.Nodes[id]
		if (n.Mark == "+" || n.Mark == "~") && len(own[id]) == 0 {
			diags = append(diags, Diag{File: n.File, Line: n.Line, Level: "warn",
				Msg: fmt.Sprintf("planned node %q has no task; add one to the tasks file", n.ID)})
		}
	}
	return diags
}
