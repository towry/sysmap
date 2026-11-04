---
name: using-sysmap
description: Reads, maintains and plans changes with a hand-written system map (.smap files) through the sysmap CLI. Use when a repository has a system map (.sysmap/system.smap or *.smap) and you must understand the system quickly, find which parts are shared boundaries versus business-specific, decide where a new feature belongs, see what a planned change touches, or update the map after a change.
---

# Using sysmap

A system map is a plain-text tree of nodes (`core` shared boundary, `biz` business-specific, `ext` outside the system), dependency edges (`a -> b`, `a ~> b` async), flows and `+`/`~` markers for planned changes. People get an interactive page (`sysmap build`); you read it with commands. Never read the HTML and avoid cat-ing the `.smap` file: the commands give you only what you ask for.

Find the tool first: `sysmap version`. If it is missing, build it from the sysmap repo (`go build -o /tmp/sysmap .`) and use that path; do not guess a flag, run `sysmap` with no arguments for the usage.

Which file: a command takes the map as an argument that ends in `.smap` or contains a `/`, in any position (`sysmap outline -d 1 map.smap`, `sysmap show ledger map.smap`; any other word is a node id). Without an argument it uses `$SYSMAP_FILE`, then `system.smap`, then `.sysmap/system.smap`. Export `SYSMAP_FILE` once if the map lives elsewhere.

## Reading a system (progressive, cheapest first)

1. `sysmap spec` once, so you know the file format and the rules.
2. `sysmap outline -d 1`: the top level, about 1 KB. Then `sysmap outline <id>` to go down one subtree.
3. `sysmap show <id>`: one node in full: description, detail lines, code refs, children, what it depends on, what uses it, flows. **The `|` detail lines carry the traps and the facts that matter ("config is ignored here", "frozen, do not extend"); they appear only in `show`, not in `outline`.** Show every node your answer depends on.
4. `sysmap find <word>`: when you do not know the node, search ids, titles, descriptions and detail lines. It returns the qualified id and `file:line`. It is a plain substring match on the words the map actually uses, and prints the first matching text under each hit (`desc:`, a `|` detail line or `title:`; an id-only hit has no text). `show` a hit before you rely on it.
   Caveats: the map marks what is unverified or undecided with detail lines starting `unverified:` or `open:`. `sysmap find --caveats` lists every node that has them with the lines, and `outline` shows ` ?N` after such nodes. Read them before you state anything about production or status; if a node says it is unverified, say so. Older maps may hedge in free prose instead, then `show` the nodes that make status claims.
5. Flows (named step sequences): `outline` lists their ids and step counts; `sysmap show <flow id>` (qualified in a composed map, e.g. `ml.daily`) prints the steps in order. A node id wins if a node and a flow share an id.
6. `sysmap impact`: what the planned (`+`/`~`) nodes reach. Only planned nodes are analysed; to ask what changing an existing node would hit, temporarily mark it `~` in a scratch copy and run `impact` on that copy.

How to read what you see:
- `←N` is the number of nodes drawn as depending on a node; a high number on a `core` node marks a common boundary. It counts only edges someone drew, so a missing edge makes shared code look unused.
- `[core]` means shared: changing it ripples out. `[biz]` is specific to one area. `[ext]` is outside the system.
- Confidence words (for example "unverified", "from reading the code", "not found") are written as prose in descriptions and detail lines. Read them; do not treat a node as a verified fact when it says otherwise.
- `check` warns about a business node placed inside a shared node and about dependency cycles (one warning per group of nodes). A cycle that is deliberate, such as a build-time import against a runtime check, is confirmed by a `| cycle: <reason>` detail line on one of its nodes; read that reason, it is the explanation. Do not add a `| cycle:` line just to silence a warning: break the cycle or explain it honestly.
- Only `check` prints each warning. Other commands print one line (`note: N warnings (run 'sysmap check' to see them)`); errors are always printed in full. A known, accepted warning belongs to the map, not to your change: compare `check` before and after your edit and only act on new ones.
- `sysmap check` printing `ok` only means the map is consistent with itself. It does not prove the map matches the code. Run `sysmap check --refs` to confirm every `@ path:line` still exists.

## Deciding where a feature goes (before writing code)

1. `outline`, then `show` on the areas you might touch. Prefer reusing an existing `core` node over a new one.
2. Work on a scratch copy of the map (`cp map.smap /tmp/plan.smap`), or edit the real one if the change is going to ship with it. Add `+ biz refund "Refund" : ...` for new nodes, give each its edges to what it will depend on, **and mark `~` every existing node the change will edit, shared ones above all**: `impact` only knows about marks, so a forgotten `~` on a shared node turns a wide change into a harmless-looking "contained" one. If a new node needs an existing node to know about it (a registry, a runner that must load it), that existing node is changing: mark it `~`.
3. `sysmap check`, then `sysmap impact`. `impact` also lists planned nodes already in the map from earlier work; look for yours. Read the level it reports: **extension** (only new business nodes reusing shared ones, nothing existing changes) is the healthy shape; **contained** touches business nodes or makes an existing node depend on the new one; **wide** changes a shared node and lists the whole chain of dependents. Read the notes too: a new node that depends on a business node of another area, or that declares no dependency, is usually in the wrong place.
4. If the result is wider than the feature deserves, redesign the node placement and rerun before coding.
5. After the change ships: `sysmap accept` removes the markers in every file; update details and refs so the map still tells the truth; run `sysmap check --refs`.

## Writing and editing a map

- Run `sysmap spec` for the exact syntax. **Top-level lines start in column 0**, children are indented two spaces, never tabs. Ids use ASCII letters/digits/`_`/`-` (no dots), one line per node: `core ledger "Ledger" : the only door for money movement`.
- Where new lines go: nodes and edges go before the first `flow` line, because every indented line after `flow` is read as a step of that flow (`inside a flow only steps like ...`). A top-level node written with two spaces of indent is silently read as a child of the node above it; if `impact` or `check` mentions an unexpected parent or area, check your indentation first. A flood of `indent jumps a level` errors means the same mistake.
- Keep each level to about 5 to 15 nodes; push detail down into children or `|` lines. Put the "why" and the traps in `|` lines, and where the code is in `@ path:line`. A Mermaid diagram is also an `@` ref: `@ docs/state.mmd` (or `.mermaid`) loads the file, resolved from the map's directory first and then the repository root. A missing diagram fails `check` with its line; the page shows it in the detail rail and on the canvas.
- Classify honestly: `core` only if several areas really share it. Record what you could not verify as a detail line `| unverified: <what and why>` (or `| open: <the undecided question>`) on the node, so readers can list it with `sysmap find --caveats`; do not leave gaps out.
- Draw edges at the level that matters. After editing, run `sysmap check`.

## Large systems: several files with mount and bind

Give each subsystem its own `.smap` next to its code and mount it from a root map under a node:

```
biz ml "ML pipeline" : research to the daily run
  mount ../docs/ml.smap
  bind postgres = pg
ext pg "Postgres" : shared database
```

Inside the mounted file, everything outside that subsystem (a shared database, another subsystem) is a local `ext` stub with normal edges; the mounting file joins it with `bind <stub> = <target>`, written directly under the `mount` line. `<stub>` is the stub's local id in the mounted file (also when it sits inside an `ext` container). `<target>` is a node the mounting file can see, by its id in that file (`pg`), or a node inside another mounted file by its qualified id (`orders.place_order`). Example of a subsystem calling into a sibling:

```
# root.smap                      # billing.smap
biz orders "Orders"              biz charge "Charge"
  mount orders.smap              ext orders_api "Orders API"
biz billing "Billing"            charge -> orders_api : place order
  mount billing.smap
  bind orders_api = orders.place_order
```

In the composed map ids are prefixed (`ml.deploy_svc`); `sysmap show ml.postgres` still resolves a bound stub. A file never draws an edge to an id inside a file it mounts. The root map should stay at 7 to 15 nodes. Every command works on the composed map; `check` reports each problem with `file:line`, and a child file passes `check` on its own.

## What the map cannot do

- It does not read code. If a change adds a dependency in code that the map does not show, the map is wrong, not the code. Review the real imports and calls against the edges.
- Keep the map and the change in the same pull request.
