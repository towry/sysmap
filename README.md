# sysmap

A map of a system that an agent can maintain in a small text file and a person can browse in one HTML page.

- **Outline first, zoom in on demand.** `sysmap outline` prints the tree; `sysmap show <id>` prints one node in full; `sysmap find <word>` searches ids, titles, descriptions and details. The page does the same: an outline on the left, one level of the map in the middle, details on the right. Double-click a node to go inside it.
- **Shared boundary or business node.** Every node is `core` (shared by many), `biz` (business-specific) or `ext` (outside the system). The page shows how many nodes depend on each shared node.
- **One file per subsystem.** A node can `mount` another `.smap`; the mounted file's top-level nodes become its children under dotted ids (`ml.deploy_svc`). The child writes its outside dependencies as local `ext` stubs, and the parent joins them to real nodes with `bind`. Each file can still be checked alone.
- **Impact of a planned change.** Mark a node `+` (new) or `~` (changed) and `sysmap impact`, and the page, show whether the change only reuses shared nodes or reaches into them.
- **Checked, not banned.** `sysmap check` warns when a business node sits directly inside a shared node, and once per dependency cycle. A deliberate cycle is confirmed with a `| cycle: <reason>` detail line on any node in it; a mounted file's top level is judged inside its own file.
- **Flows and hedged claims are easy to inspect.** `sysmap show <flow>` prints a flow's steps with their source locations. Mark uncertain claims with `| unverified: <reason>` or `| open: <question>`; `sysmap find --caveats` lists them and `outline` shows a trailing `?N` on each affected node.
- **Mermaid diagrams stay next to the map.** A node can refer to one or more diagram files with `@ docs/state.mmd` (or `.mermaid`); the page shows a preview in the detail rail and draws each on the canvas with pan and zoom. The file text is loaded at build and serve time, so `sysmap serve` reloads when the diagram changes. Links and images inside a diagram are not loaded. The renderer is embedded only when the map has a diagram.
- **No code analysis.** The map is written by the agent from what it read; sysmap itself has no code parsers and makes no network calls. Diagram files are rendered locally by the embedded viewer.

```
sysmap spec                       # the file format
sysmap outline [file] [-d N] [id] # tree, default depth 2
sysmap show <id|flow> [file]      # one node, or a flow and its steps
sysmap find <word> [file]         # search ids, titles, descriptions, detail lines
sysmap find --caveats [file]      # list nodes with unverified: or open: caveats
sysmap check [file] [--refs]      # references, boundary rules, cycles, @ code refs
sysmap impact [file] [--json]     # reach of planned nodes
sysmap accept [file]              # drop + and ~ after the change shipped
sysmap build [file] -o map.html   # one shareable HTML file, no network needed
sysmap serve [file]               # browser view that follows edits (127.0.0.1:4317)
```

`file` defaults to `$SYSMAP_FILE`, `system.smap`, then `.sysmap/system.smap`. Node ids are local to their file and may not contain a dot; a mounted node is addressed as `<mount>.<id>`. Map lines start in column 0; indent a child by two spaces. `sysmap check` prints the diagnostics (repeated messages collapse after three); other commands print errors and a one-line warning summary. `find` shows the matching description, detail or title under each hit. See [examples/shop.smap](examples/shop.smap), and run `sysmap spec` for `mount` and `bind`.

## Install

```
go install github.com/towry/sysmap@latest
```

## Develop

```
go test ./...
go run . serve examples/shop.smap
```

The page lays out nodes with [dagre](https://github.com/dagrejs/dagre) and pans and zooms with [@panzoom/panzoom](https://github.com/timmywil/panzoom) (both MIT), vendored in `internal/smap/vendor/` and inlined into every built page. Mermaid diagrams render with [Pele](https://github.com/obsidianmd/pele) (MIT), also vendored there and inlined only when the map has at least one diagram; see the README there for how to update them.

In the page: drag the background to pan, pinch or hold Ctrl/Cmd and scroll to zoom, two-finger scroll pans, `+` `-` `0` zoom and reset, double-click a node to go inside it, `Esc` to go back. A node with a diagram shows a card in the right rail; click "在画布中查看" to open it and `Esc` to return to the architecture graph.
