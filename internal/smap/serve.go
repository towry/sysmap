package smap

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Serve shows the file in the browser and re-reads it on every request, so the page
// follows edits. A file with errors keeps the last good model and reports the errors.
func Serve(path, addr string) error {
	var mu sync.Mutex
	var last *Model
	var lastSource string

	current := func() (string, *Model, []Diag, error) {
		src, m, diags, err := Load(path)
		if err != nil {
			return "", nil, nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if HasErrors(diags) {
			if last == nil {
				var lines []string
				for _, d := range diags {
					lines = append(lines, fmt.Sprintf("%s:%d: %s: %s", path, d.Line, d.Level, d.Msg))
				}
				return src, m, diags, fmt.Errorf("%s has errors and there is no earlier good version:\n%s", path, strings.Join(lines, "\n"))
			}
			return lastSource, last, diags, nil
		}
		last, lastSource = m, src
		return src, m, diags, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		src, m, diags, err := current()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(m.HTML(src, diags, true))
	})
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		src, m, diags, err := current()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(m.JSON(src, diags))
	})
	fmt.Printf("serving %s on http://%s\n", path, addr)
	return http.ListenAndServe(addr, mux)
}
