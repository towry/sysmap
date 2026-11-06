package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/towry/sysmap/internal/smap"
)

var version = "dev" // set with -ldflags "-X main.version=..."; go install builds report the module version

const usage = `sysmap: a system map an agent can maintain and a person can browse.

  sysmap outline [file] [-d N] [id]   tree of the system (default depth 2), or of one node
  sysmap show <id|flow> [file]        zoom into a node, or print a flow's steps
  sysmap find <query> [file]          search ids, titles, descriptions and detail lines
  sysmap find --caveats [file]        list nodes with unverified: or open: caveats
  sysmap check [file] [--refs]        validate references, boundary rules and @ code refs
  sysmap impact [file] [--json]       reach of planned nodes (+ new, ~ changed)
  sysmap accept [file]                drop the + and ~ markers in every file once shipped
  sysmap build [file] [-o out.html]   write one shareable HTML file
  sysmap serve [file] [-addr host:port]  show in the browser and follow edits
  sysmap json [file]                  the model as JSON
  sysmap spec                         the file format, for writing a map
  sysmap version

file ends in .smap or contains a /; any other word is a node id (for find, the query).
A node id is local to its file and may not contain a dot; a mounted node is addressed as
<mount>.<id>, and a bind target may be dotted. file defaults to $SYSMAP_FILE, system.smap,
then .sysmap/system.smap. --refs resolves @ path[:line] from the nearest ancestor of the
map file that contains .git, or from the map file's own directory.
--tasks <file> joins a Markdown checklist for serve, build, json and check; it is never
read without the flag and never changes the .smap.
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	smap.SetProgramVersion(version)
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println(smap.ProgramVersion)
		return 0
	case "spec":
		fmt.Print(smap.Spec)
		return 0
	}

	switch cmd {
	case "outline", "show", "find", "check", "impact", "accept", "build", "serve", "json":
	default:
		return fail("unknown command %q\n\n%s", cmd, usage)
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	depth := fs.Int("d", 2, "outline depth")
	out := fs.String("o", "sysmap.html", "output file")
	addr := fs.String("addr", "127.0.0.1:4317", "listen address")
	asJSON := fs.Bool("json", false, "machine-readable output")
	refs := fs.Bool("refs", false, "verify @ path[:line] references")
	caveats := fs.Bool("caveats", false, "list nodes with unverified: or open: caveats")
	tasks := fs.String("tasks", "", "tasks checklist file")
	positional, err := parseInterleaved(fs, rest)
	if err != nil {
		return 2
	}
	if *tasks != "" && cmd != "serve" && cmd != "build" && cmd != "json" && cmd != "check" {
		return fail("--tasks is only supported by serve, build, json and check")
	}

	// show takes an id first; outline takes an optional id last. Anything that looks like a
	// file (has a path separator or the .smap suffix) is the file, the other word is the id.
	// find instead joins its words, so a query may be several words when quoted.
	var file, id string
	var words []string
	for _, p := range positional {
		switch {
		case strings.Contains(p, "/") || strings.HasSuffix(p, ".smap"):
			file = p
		case cmd == "find":
			words = append(words, p)
		case id == "":
			id = p
		default:
			return fail("unexpected argument %q", p)
		}
	}
	query := strings.Join(words, " ")
	if cmd == "find" {
		if *caveats {
			if query != "" {
				return fail("find --caveats takes no search word")
			}
		} else if query == "" {
			return fail("find needs a search word")
		}
	} else if cmd != "show" && cmd != "outline" && id != "" {
		file, id = id, ""
	}
	path, err := smap.Resolve(file)
	if err != nil {
		return fail("%v", err)
	}

	switch cmd {
	case "accept":
		_, m, diags, err := smap.Load(path)
		if err != nil {
			return fail("%v", err)
		}
		printDiagnostics(path, false, diags)
		if smap.HasErrors(diags) {
			return 1
		}
		n, err := smap.AcceptFiles(m.Files)
		if err != nil {
			return fail("%v", err)
		}
		if len(m.Files) == 1 {
			fmt.Printf("removed %d marker(s) in %s\n", n, path)
		} else {
			fmt.Printf("removed %d marker(s) in %d files\n", n, len(m.Files))
		}
		return 0
	case "serve":
		if err := smap.Serve(path, *tasks, *addr); err != nil {
			return fail("%v", err)
		}
		return 0
	}

	src, m, diags, err := smap.Load(path)
	if err != nil {
		return fail("%v", err)
	}
	if cmd == "check" && *refs {
		diags = append(diags, m.CheckRefs()...)
	}
	var overlay *smap.TaskOverlay
	if *tasks != "" {
		taskSrc, list, taskDiags, err := smap.LoadTasks(*tasks)
		if err != nil {
			return fail("cannot read tasks %s: %v", *tasks, err)
		}
		overlay = &smap.TaskOverlay{Source: taskSrc, Tasks: list}
		diags = append(diags, taskDiags...)
		if cmd == "check" {
			diags = append(diags, m.CheckTasks(list, *tasks)...)
		}
	}
	printDiagnostics(path, cmd == "check", diags)
	if smap.HasErrors(diags) {
		return 1
	}

	switch cmd {
	case "check":
		fmt.Printf("ok: %d nodes, %d edges, %d flows\n", len(m.Nodes), len(m.Edges), len(m.Flows))
		fmt.Print(m.VersionText())
	case "outline":
		text, err := m.Outline(id, *depth)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Print(text)
	case "show":
		if id == "" {
			return fail("show needs a node or flow id")
		}
		text, err := m.Show(id)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Print(text)
	case "find":
		if *caveats {
			fmt.Print(m.FindCaveats())
		} else {
			fmt.Print(m.Find(query))
		}
	case "impact":
		imp := m.Impact()
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(imp); err != nil {
				return fail("%v", err)
			}
			return 0
		}
		fmt.Print(imp.Text())
	case "json":
		fmt.Println(string(m.JSONWithTasks(src, overlay, diags)))
	case "build":
		if err := os.WriteFile(*out, m.HTMLWithTasks(src, overlay, diags, false), 0o644); err != nil {
			return fail("%v", err)
		}
		fmt.Printf("wrote %s\n", *out)
	}
	return 0
}

// diagnosticLines formats diagnostics for stderr. check prints every warning; other
// commands print errors only and summarize warnings, since the full list is one `check`
// away. More than three diagnostics with the same text in one file collapse after three.
func diagnosticLines(path string, all bool, diags []smap.Diag) []string {
	type key struct{ file, msg string }
	display := func(d smap.Diag) string {
		if d.File != "" {
			return d.File
		}
		return path
	}
	var shown []smap.Diag
	warnings := 0
	for _, d := range diags {
		if d.Level == "warn" {
			warnings++
			if !all {
				continue
			}
		}
		shown = append(shown, d)
	}
	total := map[key]int{}
	for _, d := range shown {
		total[key{display(d), d.Msg}]++
	}
	seen := map[key]int{}
	var lines []string
	for _, d := range shown {
		file := display(d)
		k := key{file, d.Msg}
		seen[k]++
		if seen[k] > 3 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s:%d: %s: %s", file, d.Line, d.Level, d.Msg))
		if seen[k] == 3 && total[k] > 3 {
			lines = append(lines, fmt.Sprintf("(and %d more like this in %s)", total[k]-3, file))
		}
	}
	if !all && warnings > 0 {
		word := "warnings"
		if warnings == 1 {
			word = "warning"
		}
		lines = append(lines, fmt.Sprintf("note: %d %s (run 'sysmap check' to see them)", warnings, word))
	}
	return lines
}

func printDiagnostics(path string, all bool, diags []smap.Diag) {
	for _, line := range diagnosticLines(path, all, diags) {
		fmt.Fprintln(os.Stderr, line)
	}
}

// parseInterleaved lets flags appear before or after positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "sysmap: "+format+"\n", args...)
	return 1
}
