package smap

// Spec is printed by `sysmap spec`, so an agent never has to guess the format.
const Spec = `sysmap file format (*.smap): plain text, 2-space indent, # starts a comment.

  system "Name" : one line on what the system is

  core platform "Platform" : shared by many business nodes; change with care
    core ledger "Ledger" : the only door for money movement
      | extra lines of detail, shown when you zoom in
      @ path/to/file.ex:12          (where to read the code)
  biz billing "Billing" : business-specific
    biz invoice "Invoice" : only it may mark an order invoiced
  ext stripe "Stripe" : outside the system

  invoice -> ledger : post entry      (-> call, ~> async)

  flow checkout "Checkout" : optional note
    cart -> invoice : submit order    (steps follow the edges above)

kinds
  core  shared boundary, used across business nodes
  biz   business-specific node
  ext   system outside the one being mapped

structure
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

reading the outline
  ←N     N nodes outside this one depend on it (a container and its child count once)
  (+N below)   N descendants hidden by the depth limit; use 'sysmap show <id>' or -d to see them

planning a change (impact shows up in the page and in 'sysmap impact')
  + biz refund "Refund" : new node, not built yet
  ~ core ledger "Ledger" : existing node that the change will modify
  Add the edges the new node will have. After the change ships, run 'sysmap accept'.

rules checked by 'sysmap check'
  error  unknown node in an edge or flow step, duplicate id, bad indent
         mount path missing, absolute or a URL; mount cycle; bad bind stub or target
  warn   a core node depends on a business node
         a flow step with no matching edge
  --refs also checks each @ path[:line]: the path must exist and a line number must not
         pass the end of a regular file. Paths resolve from the nearest ancestor of the
         map file that contains .git, or from the map file's own directory.
`
