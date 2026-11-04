# sysmap

A map of a system that an agent can maintain in a small text file and a person can browse in one HTML page.

- **Outline first, zoom in on demand.** `sysmap outline` prints the tree; `sysmap show <id>` prints one node in full. The page does the same: an outline on the left, one level of the map in the middle, details on the right. Double-click a node to go inside it.
- **Shared boundary or business node.** Every node is `core` (shared by many), `biz` (business-specific) or `ext` (outside the system). The page shows how many nodes depend on each shared node.
- **Impact of a planned change.** Mark a node `+` (new) or `~` (changed) and `sysmap impact`, and the page, show whether the change only reuses shared nodes or reaches into them.
- **No code analysis.** The map is written by the agent from what it read; there are no parsers and no network calls.

```
sysmap spec                       # the file format
sysmap outline [file] [-d N] [id] # tree, default depth 2
sysmap show <id> [file]           # one node: details, children, dependencies, flows
sysmap check [file]               # references and boundary rules
sysmap impact [file] [--json]     # reach of planned nodes
sysmap accept [file]              # drop + and ~ after the change shipped
sysmap build [file] -o map.html   # one shareable HTML file, no network needed
sysmap serve [file]               # browser view that follows edits (127.0.0.1:4317)
```

`file` defaults to `$SYSMAP_FILE`, `system.smap`, then `.sysmap/system.smap`. See [examples/shop.smap](examples/shop.smap).

## Install

```
go install github.com/towry/sysmap@latest
```

## Develop

```
go test ./...
go run . serve examples/shop.smap
```

The page lays out nodes with [dagre](https://github.com/dagrejs/dagre) and pans and zooms with [@panzoom/panzoom](https://github.com/timmywil/panzoom) (both MIT), vendored in `internal/smap/vendor/` and inlined into every built page; see the README there for how to update them.

In the page: drag the background to pan, pinch or hold Ctrl/Cmd and scroll to zoom, two-finger scroll pans, `+` `-` `0` zoom and reset, double-click a node to go inside it, `Esc` to go back.
