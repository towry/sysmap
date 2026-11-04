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
  Indent a node under another to contain it. Ids are unique across the file
  (ASCII letters, digits, . _ -). The title in quotes and the " : description" are optional;
  a title cannot contain a double quote.
  Draw edges between siblings or at the level that matters; an edge to a node also
  counts as using its parent. Keep each level to about 5-15 nodes; push detail down.

reading the outline
  ←N     N nodes outside this one depend on it (a container and its child count once)
  (+N below)   N descendants hidden by the depth limit; use 'sysmap show <id>' or -d to see them

planning a change (impact shows up in the page and in 'sysmap impact')
  + biz refund "Refund" : new node, not built yet
  ~ core ledger "Ledger" : existing node that the change will modify
  Add the edges the new node will have. After the change ships, run 'sysmap accept'.

rules checked by 'sysmap check'
  error  unknown node in an edge or flow step, duplicate id, bad indent
  warn   a core node depends on a business node
         a flow step with no matching edge
`
