package smap

// Spec is printed by `sysmap spec`, so an agent never has to guess the format.
const Spec = `sysmap file format (*.smap): plain text, 2-space indent, # starts a comment.

# sysmap-format: 1
# sysmap-reviewed-with: v0.1.2
system "Name" : one line on what the system is

core platform "Platform" : shared by many business nodes; change with care
  core ledger "Ledger" : the only door for money movement
    | extra lines of detail, shown when you zoom in
    @ path/to/file.ex:12          (where to read the code)
    @ docs/state.mmd              (a Mermaid diagram; .mmd or .mermaid)
biz billing "Billing" : business-specific
  biz invoice "Invoice" : only it may mark an order invoiced
ext stripe "Stripe" : outside the system

invoice -> ledger : post entry      (-> call, ~> async)

flow checkout "Checkout" : optional note
  cart -> invoice : submit order    (steps follow the edges above)

version declarations
  Format 1 uses the format supported by Sysmap v0.1.2 as its baseline. Each file may
  declare format 1 and the stable Sysmap release it was last reviewed with.
  Put each declaration at most once before map content; do not guess a historical version.
  Run 'sysmap check --refs' and review this spec before recording a reviewed-with version.
  Declarations do not prove the map matches the code. Old Sysmap releases ignore these
  comments and cannot be relied on to reject unsupported future formats.

kinds
  core  shared boundary, used across business nodes
  biz   business-specific node
  ext   system outside the one being mapped

structure
  Top-level lines start in column 0; children are indented two spaces.
  Indent a node under another to contain it. Ids are unique inside the file
  (ASCII letters, digits, _ -; a dot is reserved for a node inside a mounted file).
  The title in quotes and the " : description" are optional; a title cannot contain a
  double quote. Draw edges between siblings or at the level that matters; an edge to a
  node also counts as using its parent. Keep each level to about 5-15 nodes; push detail down.

multi-file maps: mount a whole subsystem
core ml "ML pipeline" : research to the daily run
  mount ../docs/architecture/ml.smap
  bind postgres = platform_pg
ext platform_pg "Postgres" : shared database

  mount <path>   the mounted file's top-level nodes become children of this node. The path
                 is relative to the file that mounts it; an absolute path, a URL, a cycle
                 and nesting deeper than 8 mounts are errors. A node mounts at most one file.
  bind <stub> = <target>
                 join an ext leaf stub inside the mounted file to a node here or in another
                 mounted file (signal.ingest). After composition the stub disappears, its
                 edges and flow steps point at the target, and show ml.<stub> still resolves.
  Ids inside a file are prefixed with the mounting node: ml.deploy_svc. While that file is
  written or checked alone they stay local. A file may draw edges to its own nodes and to
  the node it mounts, never to an id inside the mounted file: declare a local ext stub in
  the child and bind it here. 'sysmap find <word>' searches ids, titles, descriptions and
  detail lines across the composed map.

diagrams
  Each @ ref whose path ends in .mmd or .mermaid loads a Mermaid diagram file. A node may
  list several diagram refs; the page shows a card for each. The path is relative to the
  .smap file first, then the repository root. The page shows a preview in the right rail
  and can draw the diagram on the canvas; 'sysmap serve' reloads when the diagram changes.
  A missing diagram is an error on the ref line.
  A diagram must stay inside the repository root: the nearest ancestor of the map file
  that has .git, or the map file's own directory when there is none. Absolute paths and
  URLs are refused. Diagram links and images are not loaded; a URL in a label stays text.

tasks file (--tasks)
  sysmap serve|build|json|check <map> --tasks <file> joins a Markdown checklist at view
  time. Without --tasks nothing is read or shown; no tasks file is discovered automatically.
  - [ ] refund: write the refund path
  - [~] refund: expose the API
  - [x] refund: post the ledger entry
  Blank lines and # comments are ignored. The text before the first colon is a node id
  (dotted for a node inside a mounted file); the rest is the task. [ ] is todo, [~] is
  doing, [x] is done. Every line must have the '- [ ] node: text' shape; a bad line is an
  error with its line number. A node's badge is done/total over its own tasks and every
  descendant's tasks; a node with no tasks shows no badge. 'check --tasks' warns for a
  task naming an unknown node and for a + or ~ node with no task. A missing or unreadable
  tasks file is an error. The tasks file is view-time only: create it for a change, tick
  it as work goes, and delete it after 'sysmap accept'.

reading the outline
  ←N     N nodes outside this one depend on it (a container and its child count once)
  ?N     N unverified: or open: caveat lines on this node; see 'sysmap find --caveats'
  (+N below)   N descendants hidden by the depth limit; use 'sysmap show <id>' or -d to see them
  show <id> reads a node; show <flow> prints a flow's steps with file:line.

planning a change (impact shows up in the page and in 'sysmap impact')
+ biz refund "Refund" : new node, not built yet
~ core ledger "Ledger" : existing node that the change will modify
  Add the edges the new node will have. A + node may already list @ path refs for files it
  will create. After the change ships, run 'sysmap accept': it checks that no + or ~ node
  still has an open: caveat or a missing @ ref, then drops + and ~.

deliberate dependency cycles
  check warns once per group of nodes that depend on each other in a cycle. If the cycle
  is intended, confirm it with a detail line on any node in the group:
biz a "A"
  | cycle: both directions are required during the migration
  The text after "cycle:" must be non-empty; an empty "| cycle:" does not confirm it.
  Self-loops and edges between a container and its own child are reported separately.

hedged claims (caveats)
  A detail line starting with unverified: or open: followed by non-empty text marks a
  caveat on that node, for example:
biz a "A"
  | unverified: no load test yet
  'sysmap find --caveats' lists the nodes and their caveat lines; outline adds a trailing
  ?N with N caveat lines on the node. These are reading aids, not warnings.

rules checked by 'sysmap check'
  error  unknown node in an edge or flow step, duplicate id, bad indent
         mount path missing, absolute or a URL; mount cycle; bad bind stub or target
         a referenced .mmd or .mermaid diagram file is missing
  warn   a core node depends on a business node
         a business node sits directly inside a shared node (a mounted file's top level
         is judged inside that file, not by the kind of its mount node)
         a flow step with no matching edge
         a dependency cycle between nodes, unless one of them has "| cycle: <reason>"
         a node that reads two or more independently fed core stores (~>), unless each store
         has "| scope: <coverage, schedule, basis>" and the reader has "| join: <rule>"
  --tasks also warns: a task names an unknown node; a + or ~ node has no task
  --refs also checks each @ path[:line]: the path must stay inside the repository root,
         exist (unless the node is marked +), and a line number must not pass the end of
         a regular file. Paths resolve from the nearest ancestor of the map file that
         contains .git, or from the map file's own directory.
  --touched <files> also warns when a touched code file belongs to an unmarked node
         (missing ~) or is not covered by any @ ref in the map.
`
