package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/towry/sysmap/internal/smap"
)

var version = "dev" // set with -ldflags "-X main.version=..."; go install builds report the module version

const usage = `sysmap: a system map an agent can maintain and a person can browse.

  sysmap outline [file] [-d N] [id]   tree of the system (default depth 2), or of one node
  sysmap show <id> [file]             zoom into one node: details, children, dependencies, flows
  sysmap check [file]                 validate references and boundary rules
  sysmap impact [file] [--json]       reach of planned nodes (+ new, ~ changed)
  sysmap accept [file]                drop the + and ~ markers once the change has shipped
  sysmap build [file] [-o out.html]   write one shareable HTML file
  sysmap serve [file] [-addr host:port]  show in the browser and follow edits
  sysmap json [file]                  the model as JSON
  sysmap spec                         the file format, for writing a map
  sysmap version

file ends in .smap or contains a /; any other word is a node id.
file defaults to $SYSMAP_FILE, system.smap, then .sysmap/system.smap.
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		if info, ok := debug.ReadBuildInfo(); ok && version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		fmt.Println(version)
		return 0
	case "spec":
		fmt.Print(smap.Spec)
		return 0
	}

	switch cmd {
	case "outline", "show", "check", "impact", "accept", "build", "serve", "json":
	default:
		return fail("unknown command %q\n\n%s", cmd, usage)
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	depth := fs.Int("d", 2, "outline depth")
	out := fs.String("o", "sysmap.html", "output file")
	addr := fs.String("addr", "127.0.0.1:4317", "listen address")
	asJSON := fs.Bool("json", false, "machine-readable output")
	positional, err := parseInterleaved(fs, rest)
	if err != nil {
		return 2
	}

	// show takes an id first; outline takes an optional id last. Anything that looks like a
	// file (has a path separator or the .smap suffix) is the file, the other word is the id.
	var file, id string
	for _, p := range positional {
		if strings.Contains(p, "/") || strings.HasSuffix(p, ".smap") {
			file = p
		} else if id == "" {
			id = p
		} else {
			return fail("unexpected argument %q", p)
		}
	}
	if cmd != "show" && cmd != "outline" && id != "" {
		file, id = id, ""
	}
	path, err := smap.Resolve(file)
	if err != nil {
		return fail("%v", err)
	}

	switch cmd {
	case "accept":
		src, err := os.ReadFile(path)
		if err != nil {
			return fail("%v", err)
		}
		next, n := smap.Accept(string(src))
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return fail("%v", err)
		}
		fmt.Printf("removed %d marker(s) in %s\n", n, path)
		return 0
	case "serve":
		if err := smap.Serve(path, *addr); err != nil {
			return fail("%v", err)
		}
		return 0
	}

	src, m, diags, err := smap.Load(path)
	if err != nil {
		return fail("%v", err)
	}
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "%s:%d: %s: %s\n", path, d.Line, d.Level, d.Msg)
	}
	if smap.HasErrors(diags) {
		return 1
	}

	switch cmd {
	case "check":
		fmt.Printf("ok: %d nodes, %d edges, %d flows\n", len(m.Nodes), len(m.Edges), len(m.Flows))
	case "outline":
		text, err := m.Outline(id, *depth)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Print(text)
	case "show":
		if id == "" {
			return fail("show needs a node id")
		}
		text, err := m.Show(id)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Print(text)
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
		fmt.Println(string(m.JSON(src, diags)))
	case "build":
		if err := os.WriteFile(*out, m.HTML(src, diags, false), 0o644); err != nil {
			return fail("%v", err)
		}
		fmt.Printf("wrote %s\n", *out)
	}
	return 0
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
