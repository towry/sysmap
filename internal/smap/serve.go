package smap

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Serve shows the file in the browser and re-reads it on every request, so the page
// follows edits. The tasks file, when given, is re-read the same way. A file with errors
// keeps the last good model and reports the errors.
func Serve(path, tasksPath, addr string) error {
	var mu sync.Mutex
	type snapshot struct {
		src   string
		m     *Model
		tasks *TaskOverlay
	}
	var last snapshot
	var haveLast bool

	current := func() (string, *Model, *TaskOverlay, []Diag, error) {
		src, m, diags, err := Load(path)
		if err != nil {
			return "", nil, nil, nil, err
		}
		var tasks *TaskOverlay
		if tasksPath != "" {
			taskSrc, list, taskDiags, err := LoadTasks(tasksPath)
			if err != nil {
				return "", nil, nil, nil, fmt.Errorf("cannot read tasks %s: %w", tasksPath, err)
			}
			tasks = &TaskOverlay{Source: taskSrc, Tasks: list}
			diags = append(diags, taskDiags...)
		}
		mu.Lock()
		defer mu.Unlock()
		if HasErrors(diags) {
			if !haveLast {
				var lines []string
				for _, d := range diags {
					file := d.File
					if file == "" {
						file = path
					}
					lines = append(lines, fmt.Sprintf("%s:%d: %s: %s", file, d.Line, d.Level, d.Msg))
				}
				return src, m, tasks, diags, fmt.Errorf("no earlier good version; fix these errors:\n%s", strings.Join(lines, "\n"))
			}
			return last.src, last.m, last.tasks, diags, nil
		}
		last = snapshot{src: src, m: m, tasks: tasks}
		haveLast = true
		return src, m, tasks, diags, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		src, m, tasks, diags, err := current()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(m.HTMLWithTasks(src, tasks, diags, true))
	})
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		src, m, tasks, diags, err := current()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(m.JSONWithTasks(src, tasks, diags))
	})
	if tasksPath != "" {
		fmt.Printf("serving %s with %s on http://%s\n", path, tasksPath, addr)
	} else {
		fmt.Printf("serving %s on http://%s\n", path, addr)
	}
	return http.ListenAndServe(addr, mux)
}
