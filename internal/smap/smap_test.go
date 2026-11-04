package smap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const shop = `
system "Shop"
core platform "Platform"
  core ledger "Ledger" : money door
    | detail one
    @ app/ledger.ex:1
  core events "Events"
biz orders "Orders"
  biz checkout "Checkout"
biz billing "Billing"
  biz payment "Payment"
  biz invoice "Invoice"
ext stripe "Stripe"

checkout -> payment : charge
payment -> ledger : post
invoice -> ledger : post
payment -> stripe : charge
checkout ~> events : order.changed

flow place "Place"
  checkout -> payment : charge
  payment -> ledger : post
`

func mustParse(t *testing.T, src string) *Model {
	t.Helper()
	m, diags := Parse(src)
	if HasErrors(diags) {
		t.Fatalf("parse errors: %v", diags)
	}
	if d := m.Check(); HasErrors(d) {
		t.Fatalf("check errors: %v", d)
	}
	return m
}

func TestParseTreeDetailsAndEdges(t *testing.T) {
	m := mustParse(t, shop)
	if got := m.Nodes["ledger"].Parent; got != "platform" {
		t.Fatalf("ledger parent = %q", got)
	}
	if n := m.Nodes["ledger"]; len(n.Detail) != 1 || n.Detail[0] != "detail one" || n.Refs[0] != "app/ledger.ex:1" || n.Desc != "money door" {
		t.Fatalf("ledger = %+v", n)
	}
	if !m.Edges[len(m.Edges)-1].Async || m.Edges[len(m.Edges)-1].Label != "order.changed" {
		t.Fatalf("async edge not read: %+v", m.Edges[len(m.Edges)-1])
	}
	if len(m.Flows) != 1 || len(m.Flows[0].Steps) != 2 {
		t.Fatalf("flows = %+v", m.Flows)
	}
}

func TestParseReportsEveryProblemWithItsLine(t *testing.T) {
	_, diags := Parse("core a \"A\"\ncore a \"again\"\n\tcore b\n   core c\n    core d\nwhat is this\n")
	want := map[int]string{
		2: "duplicate id",
		3: "tabs",
		4: "multiple of 2",
		5: "no parent",
		6: "cannot parse",
	}
	for line, text := range want {
		found := false
		for _, d := range diags {
			if d.Line == line && strings.Contains(d.Msg, text) {
				found = true
			}
		}
		if !found {
			t.Errorf("line %d: no diagnostic containing %q in %v", line, text, diags)
		}
	}
}

func TestDetailLineWithoutANodeIsAnError(t *testing.T) {
	if _, diags := Parse("| orphan\n@ ref\n"); len(diags) != 2 || !strings.Contains(diags[0].Msg, "must follow a node") {
		t.Fatalf("diags = %v", diags)
	}
}

func TestCheckFlagsDanglingEdgesAndCoreDependingOnBiz(t *testing.T) {
	m, _ := Parse("core a\nbiz b\nbiz c\na -> b\nc -> ghost\nc -> c\n")
	var errs, warns []string
	for _, d := range m.Check() {
		if d.Level == "error" {
			errs = append(errs, d.Msg)
		} else {
			warns = append(warns, d.Msg)
		}
	}
	if len(errs) != 2 || !strings.Contains(strings.Join(errs, "|"), `"ghost"`) || !strings.Contains(strings.Join(errs, "|"), "itself") {
		t.Fatalf("errors = %v", errs)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "shared node") {
		t.Fatalf("warnings = %v", warns)
	}
}

func TestFanInCountsOutsideSourcesOfTheWholeSubtree(t *testing.T) {
	m := mustParse(t, shop)
	// payment and invoice use ledger; checkout uses events. platform contains both.
	if got := strings.Join(m.FanIn("ledger"), ","); got != "invoice,payment" {
		t.Fatalf("FanIn(ledger) = %s", got)
	}
	if got := strings.Join(m.FanIn("platform"), ","); got != "checkout,invoice,payment" {
		t.Fatalf("FanIn(platform) = %s", got)
	}
}

func TestImpactExtensionVersusSharedChange(t *testing.T) {
	ext := mustParse(t, shop+"\n+ biz refund \"Refund\"\nrefund -> ledger : reverse\nrefund -> payment : refund\n")
	imp := ext.Impact()
	if imp.Level != "extension" || len(imp.Affected) != 0 {
		t.Fatalf("a new business node that only reuses shared nodes must be an extension with no affected nodes: %+v", imp)
	}
	if got := imp.Items[0].ReusesCore; len(got) != 1 || got[0] != "ledger" {
		t.Fatalf("reuses = %v", got)
	}

	wide := mustParse(t, strings.Replace(shop, "  core ledger", "  ~ core ledger", 1))
	imp = wide.Impact()
	// checkout depends on payment, which depends on ledger: reached transitively.
	if imp.Level != "wide" || strings.Join(imp.Affected, ",") != "checkout,invoice,payment" {
		t.Fatalf("changing a shared node must reach its transitive dependents: %+v", imp)
	}
	paths := imp.Items[0].Paths
	if strings.Join(paths["checkout"], ">") != "checkout>payment>ledger" || strings.Join(paths["payment"], ">") != "payment>ledger" {
		t.Fatalf("each dependent must carry the chain to the changed node: %v", paths)
	}

	biz := mustParse(t, strings.Replace(shop, "  biz payment", "  ~ biz payment", 1))
	if got := biz.Impact(); got.Level != "contained" || strings.Join(got.Affected, ",") != "checkout" {
		t.Fatalf("changing a business node reaches only its dependents: %+v", got)
	}

	if got := mustParse(t, shop).Impact(); got.Level != "none" || len(got.Items) != 0 {
		t.Fatalf("no markers must mean no impact: %+v", got)
	}
}

func TestImpactNotesCrossBusinessCouplingAndMissingDependencies(t *testing.T) {
	m := mustParse(t, shop+"\n+ biz refund \"Refund\"\nbiz audit\n+ biz lonely\nrefund -> invoice\n")
	notes := ""
	for _, it := range m.Impact().Items {
		for _, n := range it.Notes {
			notes += it.ID + ":" + n.Text() + "|"
		}
	}
	if !strings.Contains(notes, "refund:depends on business node \"invoice\" in another area (billing)") {
		t.Fatalf("notes = %s", notes)
	}
	if !strings.Contains(notes, "lonely:declares no dependency") {
		t.Fatalf("notes = %s", notes)
	}
}

func TestAcceptDropsMarkersOnly(t *testing.T) {
	out, n := Accept("+ biz a \"A\"\n  ~ core b\ncore c : + not a marker\na -> b : ~ stays\n")
	if n != 2 || out != "biz a \"A\"\n  core b\ncore c : + not a marker\na -> b : ~ stays\n" {
		t.Fatalf("n=%d out=%q", n, out)
	}
}

func TestOutlineAndShow(t *testing.T) {
	m := mustParse(t, shop)
	out, _ := m.Outline("", 1)
	if !strings.Contains(out, `[core] platform "Platform" ←3`) || !strings.Contains(out, "(+2 below)") || strings.Contains(out, "ledger") {
		t.Fatalf("depth-1 outline must hide children but count them:\n%s", out)
	}
	show, _ := m.Show("payment")
	for _, want := range []string{"-> ledger [core] : post", "-> stripe [ext] : charge", "<- checkout [biz] : charge", "flows: place"} {
		if !strings.Contains(show, want) {
			t.Fatalf("show payment lacks %q:\n%s", want, show)
		}
	}
	if _, err := m.Show("nope"); err == nil {
		t.Fatal("unknown id must be an error")
	}
}

func TestHTMLIsSelfContainedAndSafeToEmbed(t *testing.T) {
	m := mustParse(t, "core a \"</script><b>\" : x & y\n")
	// The panzoom bundle names its repository in a comment and in an error message; neither is a request.
	page := strings.ReplaceAll(string(m.HTML("src", nil, false)), "https://github.com/timmywil/panzoom", "")
	if strings.Contains(page, "</script><b>") {
		t.Fatal("model text must not be able to close the data script tag")
	}
	for _, bad := range []string{"http://", "https://", "__DATA__", "__DAGRE__", "__TITLE__", "__LIVE__"} {
		// the dagre bundle mentions no URLs once comments are stripped
		if strings.Contains(page, bad) {
			t.Fatalf("page contains %q", bad)
		}
	}
	if !strings.Contains(page, "dagre") {
		t.Fatal("layout library is not embedded")
	}
	if !strings.Contains(page, "Copyright (c) 2012-2014 Chris Pettitt") {
		t.Fatal("the dagre licence notice must travel with every built page")
	}
	if !strings.Contains(page, "Copyright Timmy Willison") {
		t.Fatal("the panzoom copyright header must travel with every built page")
	}
}

func TestImpactExistingNodeWiredToNewNodeIsNotAnExtension(t *testing.T) {
	m := mustParse(t, "core auth\nbiz orders\n  biz checkout\nbiz web\nweb -> checkout\ncheckout -> auth\n+ biz promo\npromo -> auth\ncheckout -> promo\n")
	imp := m.Impact()
	if imp.Level != "contained" || strings.Join(imp.Summary.Wired, ",") != "checkout,web" || strings.Join(imp.Affected, ",") != "checkout,web" {
		t.Fatalf("existing nodes that point at a new node must change too: %+v", imp)
	}
	if strings.Contains(imp.Verdict, "changes no existing node") || !strings.Contains(imp.Verdict, "must change too") {
		t.Fatalf("verdict = %s", imp.Verdict)
	}
}

func TestImpactEdgeToContainerReachesItsChangedChild(t *testing.T) {
	m := mustParse(t, "biz web\nbiz orders\n  ~ biz checkout\nweb -> orders\n")
	if got := strings.Join(m.Impact().Affected, ","); got != "web" {
		t.Fatalf("an edge to the container uses its children; affected = %q", got)
	}
}

func TestImpactNamesAChangedExternalSystemAsExternal(t *testing.T) {
	imp := mustParse(t, "biz a\n~ ext s \"Stripe\"\na -> s\n").Impact()
	if !strings.Contains(imp.Verdict, "external system(s) s") || strings.Contains(imp.Verdict, "business node(s)") {
		t.Fatalf("verdict = %s", imp.Verdict)
	}
}

func TestFanInCountsAContainerAndItsChildOnce(t *testing.T) {
	m := mustParse(t, "core auth\nbiz orders\n  biz cart\nbiz billing\norders -> auth\ncart -> auth\nbilling -> auth\n")
	if got := strings.Join(m.FanIn("auth"), ","); got != "billing,orders" {
		t.Fatalf("FanIn(auth) = %s", got)
	}
}

func TestParseToleratesABOMAndExplainsTheQuoteLimit(t *testing.T) {
	if _, diags := Parse("\ufeffcore a \"A\"\r\ncore b\r\n"); len(diags) != 0 {
		t.Fatalf("a BOM and CRLF must parse: %v", diags)
	}
	_, diags := Parse("core a \"The \\\"A\\\" node\"\n")
	if len(diags) != 1 || !strings.Contains(diags[0].Msg, "double quote") {
		t.Fatalf("diags = %v", diags)
	}
}

func TestDetailLineAfterAnEdgeIsAnError(t *testing.T) {
	if _, diags := Parse("core a\ncore b\nb -> a\n| orphan\n"); len(diags) != 1 || !strings.Contains(diags[0].Msg, "must follow a node") {
		t.Fatalf("diags = %v", diags)
	}
}

func TestShowNamesTheChildAnEdgeComesFrom(t *testing.T) {
	m := mustParse(t, "core ledger\nbiz orders\n  biz checkout\n  biz cart\nbiz web\ncheckout -> ledger : post\ncart -> ledger : read\nweb -> orders : open\n")
	show, _ := m.Show("orders")
	for _, want := range []string{"-> ledger [core] (via checkout) : post", "-> ledger [core] (via cart) : read", "<- web [biz] : open"} {
		if !strings.Contains(show, want) {
			t.Fatalf("show orders lacks %q:\n%s", want, show)
		}
	}
	if direct, _ := m.Show("checkout"); strings.Contains(direct, "(via") {
		t.Fatalf("a node's own edges need no 'via':\n%s", direct)
	}
}

// --- multi-file maps: mount and bind ---

func writeMapFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadModel(t *testing.T, path string) (*Model, []Diag) {
	t.Helper()
	_, m, diags, err := Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return m, diags
}

func loadClean(t *testing.T, path string) *Model {
	t.Helper()
	m, diags := loadModel(t, path)
	if len(diags) != 0 {
		t.Fatalf("load %s: unexpected diagnostics: %v", path, diags)
	}
	return m
}

func diagWith(diags []Diag, substr string) *Diag {
	for i := range diags {
		if strings.Contains(diags[i].Msg, substr) {
			return &diags[i]
		}
	}
	return nil
}

// shopFiles is a root map with two mounted files. ml.smap has three stubs: postgres is
// top-level, cache sits inside an ext container, and queue is bound to the other file.
func shopFiles(t *testing.T) (root, ml, signal string) {
	t.Helper()
	dir := t.TempDir()
	signal = writeMapFile(t, dir, "signal.smap", `system "Signal"
biz ingest "Ingest" : takes events
ext store "Store" : signal store
ingest -> store
`)
	ml = writeMapFile(t, dir, "ml.smap", `system "ML" : model pipeline
biz deploy "Deploy" : ships models
biz train "Train" : trains models
+ biz rank "Rank" : planned
ext postgres "Postgres" : shared db
ext shared "Shared"
  ext cache "Cache"
ext queue "Queue" : signals
deploy -> postgres : read
deploy -> cache : read cache
deploy ~> queue : emit
rank -> postgres
flow daily "Daily" : every day
  deploy -> postgres : load
  deploy -> cache : read cache
`)
	root = writeMapFile(t, dir, "root.smap", `system "Shop" : root
core ml "ML pipeline" : model training and serving
  mount ml.smap
  bind postgres = db
  bind cache = db
  bind queue = signal.ingest
biz signal "Signals"
  mount signal.smap
core db "Postgres" : shared database
biz web "Web" : front end
web -> ml : requests
ml -> db : reads
flow checkout "Checkout"
  web -> ml : request
`)
	return root, ml, signal
}

func TestLoadComposesMountedFilesWithPrefixedIds(t *testing.T) {
	root, ml, signal := shopFiles(t)
	m := loadClean(t, root)

	if len(m.Files) != 3 || m.Files[0] != root || m.Files[1] != ml || m.Files[2] != signal {
		t.Fatalf("files = %v", m.Files)
	}
	for id, parent := range map[string]string{
		"ml": "ml", "ml.deploy": "ml", "ml.train": "ml", "ml.rank": "ml", "signal.ingest": "signal",
	} {
		n := m.Nodes[id]
		if n == nil {
			t.Fatalf("missing composed node %q", id)
		}
		if parent != id && n.Parent != parent {
			t.Errorf("%s parent = %q, want %q", id, n.Parent, parent)
		}
	}
	if got := m.Nodes["ml.deploy"].File; got != ml {
		t.Errorf("ml.deploy file = %q, want %q", got, ml)
	}
	if got := m.Nodes["ml.rank"].Mark; got != "+" {
		t.Errorf("ml.rank mark = %q", got)
	}
	if len(m.Nodes) != 9 {
		t.Errorf("nodes = %d, want 9 (4 root + 3 ml + 2 signal)", len(m.Nodes))
	}
	if len(m.Edges) != 7 {
		t.Errorf("edges = %d, want 7", len(m.Edges))
	}

	flow := m.Flows[1]
	if flow.ID != "ml.daily" || flow.Scope != "ml" || flow.File != ml {
		t.Fatalf("mounted flow = %+v", flow)
	}
	if got := flow.Steps[0]; got.From != "ml.deploy" || got.To != "db" {
		t.Errorf("flow step = %+v, want ml.deploy -> db", got)
	}
	if got := flow.Steps[1]; got.From != "ml.deploy" || got.To != "db" {
		t.Errorf("second flow step = %+v, want ml.deploy -> db", got)
	}
}

func TestMountedFileStaysValidOnItsOwn(t *testing.T) {
	_, ml, _ := shopFiles(t)
	m := loadClean(t, ml)
	if m.Title != "ML" {
		t.Fatalf("title = %q", m.Title)
	}
	if m.Nodes["deploy"] == nil || m.Nodes["ml.deploy"] != nil {
		t.Fatalf("a mounted file must keep local ids: %v", m.Order)
	}
	if m.Nodes["cache"].Parent != "shared" {
		t.Fatalf("cache parent = %q", m.Nodes["cache"].Parent)
	}
}

func TestBindRemovesStubAndEmptiedExtContainer(t *testing.T) {
	root, _, _ := shopFiles(t)
	m := loadClean(t, root)

	for _, id := range []string{"ml.postgres", "ml.cache", "ml.shared", "ml.queue"} {
		if m.Nodes[id] != nil {
			t.Errorf("bound stub or emptied container %q still exists", id)
		}
	}
	if got := m.Nodes["ml"].Children; strings.Join(got, ",") != "ml.deploy,ml.train,ml.rank" {
		t.Errorf("ml children = %v", got)
	}

	redirected := 0
	for _, e := range m.Edges {
		if e.From == "ml.deploy" && e.To == "db" {
			redirected++
		}
		if strings.HasPrefix(e.From, "ml.postgres") || strings.HasPrefix(e.To, "ml.postgres") ||
			strings.HasPrefix(e.From, "ml.cache") || strings.HasPrefix(e.To, "ml.cache") ||
			strings.HasPrefix(e.From, "ml.queue") || strings.HasPrefix(e.To, "ml.queue") {
			t.Errorf("edge still names a removed stub: %+v", e)
		}
	}
	if redirected != 2 {
		t.Errorf("ml.deploy -> db edges = %d, want 2 (one from postgres, one from cache)", redirected)
	}
	async := 0
	for _, e := range m.Edges {
		if e.From == "ml.deploy" && e.To == "signal.ingest" && e.Async && e.Label == "emit" {
			async++
		}
	}
	if async != 1 {
		t.Errorf("the queue stub was not redirected to signal.ingest: %+v", m.Edges)
	}
}

func TestBindAliasResolvesTheOldId(t *testing.T) {
	root, _, _ := shopFiles(t)
	m := loadClean(t, root)
	for stub, target := range map[string]string{
		"ml.postgres": "db",
		"ml.cache":    "db",
		"ml.queue":    "signal.ingest",
	} {
		if got := m.Alias[stub]; got != target {
			t.Errorf("alias %s = %q, want %q", stub, got, target)
		}
	}
	show, err := m.Show("ml.postgres")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, `[core] db "Postgres"`) {
		t.Fatalf("show of an alias did not reach the target:\n%s", show)
	}
	out, err := m.Outline("ml.postgres", 1)
	if err != nil || !strings.Contains(out, `[core] db "Postgres"`) {
		t.Fatalf("outline of an alias = %q, %v", out, err)
	}
}

func TestBindCanTargetASiblingMountDeclaredLater(t *testing.T) {
	dir := t.TempDir()
	writeMapFile(t, dir, "a.smap", `biz a1 "A1"
ext remote "Remote"
a1 -> remote : call
`)
	writeMapFile(t, dir, "b.smap", `biz thing "Thing" : the real target
`)
	root := writeMapFile(t, dir, "root.smap", `core a "A"
  mount a.smap
  bind remote = b.thing
core b "B"
  mount b.smap
`)
	m := loadClean(t, root)
	if m.Nodes["a.remote"] != nil {
		t.Fatal("remote stub still exists")
	}
	found := false
	for _, e := range m.Edges {
		if e.From == "a.a1" && e.To == "b.thing" && e.Label == "call" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a.a1 -> b.thing not found: %+v", m.Edges)
	}
	if got := m.Alias["a.remote"]; got != "b.thing" {
		t.Errorf("alias = %q", got)
	}
}

func TestBindKindChangeRunsTheEdgeRulesAgain(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "child.smap", `core worker "Worker"
ext s "Stub"
worker -> s
`)
	root := writeMapFile(t, dir, "root.smap", `biz target "Target"
core a "A"
  mount child.smap
  bind s = target
`)
	_, diags := loadModel(t, root)
	d := diagWith(diags, "shared node")
	if d == nil {
		t.Fatalf("binding an ext stub to a biz node must warn after the kind change: %v", diags)
	}
	if d.Level != "warn" || d.File != child || d.Line != 3 {
		t.Fatalf("warning = %+v, want %s:3", d, child)
	}
	if !strings.Contains(d.Msg, `"a.worker"`) || !strings.Contains(d.Msg, `"target"`) {
		t.Fatalf("warning = %s", d.Msg)
	}
}

func TestMountCycleIsAnError(t *testing.T) {
	dir := t.TempDir()
	a := writeMapFile(t, dir, "a.smap", `core a "A"
  mount b.smap
`)
	b := writeMapFile(t, dir, "b.smap", `core b "B"
  mount a.smap
`)
	_, diags := loadModel(t, a)
	d := diagWith(diags, "mount cycle")
	if d == nil {
		t.Fatalf("no cycle diagnostic: %v", diags)
	}
	if d.Level != "error" || d.File != b || d.Line != 2 {
		t.Fatalf("cycle diagnostic = %+v, want %s:2", d, b)
	}
}

func TestMissingMountedFileCarriesTheMountingFile(t *testing.T) {
	dir := t.TempDir()
	root := writeMapFile(t, dir, "root.smap", `core a "A"
  mount nope.smap
`)
	_, diags := loadModel(t, root)
	d := diagWith(diags, "cannot read mounted file")
	if d == nil || d.Level != "error" || d.File != root || d.Line != 2 {
		t.Fatalf("diagnostics = %+v", diags)
	}
}

func TestMountRejectsAbsolutePathsAndURLs(t *testing.T) {
	dir := t.TempDir()
	root := writeMapFile(t, dir, "root.smap", `core a "A"
  mount /etc/hosts
core b "B"
  mount https://example.com/b.smap
`)
	_, diags := loadModel(t, root)
	if len(diags) != 2 {
		t.Fatalf("diags = %v", diags)
	}
	for _, d := range diags {
		if d.Level != "error" || !strings.Contains(d.Msg, "must be relative") {
			t.Errorf("diag = %+v", d)
		}
		if d.File != root {
			t.Errorf("diag file = %q, want %q", d.File, root)
		}
	}
}

func TestBadBindsAreReported(t *testing.T) {
	cases := []struct {
		name, root, child, want string
	}{
		{
			name:  "second bind",
			child: "ext s \"S\"\n",
			root: `biz target "T"
core a "A"
  mount child.smap
  bind s = target
  bind s = target
`,
			want: "already bound",
		},
		{
			name:  "non ext stub",
			child: "biz s \"S\"\n",
			root: `biz target "T"
core a "A"
  mount child.smap
  bind s = target
`,
			want: "only an ext stub",
		},
		{
			name:  "non leaf stub",
			child: "ext outer \"O\"\n  ext inner \"I\"\n",
			root: `biz target "T"
core a "A"
  mount child.smap
  bind outer = target
`,
			want: "has children",
		},
		{
			name:  "unknown target",
			child: "ext s \"S\"\n",
			root: `core a "A"
  mount child.smap
  bind s = nowhere
`,
			want: "unknown target",
		},
		{
			name:  "bind without mount",
			child: "",
			root: `biz target "T"
core a "A"
  bind s = target
`,
			want: "must sit under a node that mounts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.child != "" {
				writeMapFile(t, dir, "child.smap", tc.child)
			}
			root := writeMapFile(t, dir, "root.smap", tc.root)
			_, diags := loadModel(t, root)
			if d := diagWith(diags, tc.want); d == nil {
				t.Fatalf("no diagnostic containing %q: %v", tc.want, diags)
			}
		})
	}
}

func TestDiagnosticsCarryTheRightFile(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "child.smap", `biz x "X"
biz y "Y"
x -> ghost
`)
	root := writeMapFile(t, dir, "root.smap", `core a "A"
  mount child.smap
biz w "W"
w -> a.x
`)
	_, diags := loadModel(t, root)
	if d := diagWith(diags, `"ghost"`); d == nil || d.File != child || d.Line != 3 {
		t.Fatalf("child diagnostic = %+v, want %s:3", d, child)
	}
	if d := diagWith(diags, `"a.x"`); d == nil || d.File != root || d.Line != 4 {
		t.Fatalf("root diagnostic = %+v, want %s:4", d, root)
	}
}

func TestAcceptFilesDropsMarkersInEveryFile(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "child.smap", "~ biz old \"Old\" : keep me\n")
	root := writeMapFile(t, dir, "root.smap", `core a "A"
  mount child.smap
+ biz new "New"
`)
	m, _ := loadModel(t, root)
	n, err := AcceptFiles(m.Files)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("removed %d markers, want 2", n)
	}
	for path, want := range map[string]string{
		root:  "core a \"A\"\n  mount child.smap\nbiz new \"New\"\n",
		child: "biz old \"Old\" : keep me\n",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", path, data, want)
		}
	}
}

func TestVersionHashCoversMountedFiles(t *testing.T) {
	root, ml, _ := shopFiles(t)
	version := func() string {
		src, m, diags, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(m.JSON(src, diags), &v); err != nil {
			t.Fatal(err)
		}
		return v.Version
	}
	before := version()
	data, err := os.ReadFile(ml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ml, append(data, []byte("\n# changed\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	after := version()
	if before == after {
		t.Fatalf("version %q did not change when a mounted file changed", before)
	}
}

func TestOutlineScopesFlowsAndCountsPlanned(t *testing.T) {
	root, _, _ := shopFiles(t)
	m := loadClean(t, root)

	all, err := m.Outline("", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`(+3 below, 1 planned) [mount ml.smap]`,
		`checkout "Checkout" (1 steps)`,
		`ml: 1 flows`,
		`signal: 0 flows`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("root outline lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "ml.daily") {
		t.Errorf("root outline must not list a mounted file's flow ids:\n%s", all)
	}

	scope, err := m.Outline("ml", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scope, `ml.daily "Daily" (2 steps)`) {
		t.Errorf("outline ml lacks its own flow:\n%s", scope)
	}
	if strings.Contains(scope, `checkout "Checkout"`) {
		t.Errorf("outline ml leaked a root flow:\n%s", scope)
	}
	if !strings.Contains(scope, `+ [biz] ml.rank`) {
		t.Errorf("outline ml lacks the planned child:\n%s", scope)
	}
}

func TestFindSearchesIdTitleDescriptionAndDetail(t *testing.T) {
	dir := t.TempDir()
	path := writeMapFile(t, dir, "find.smap", `system "Find"
biz alpha "Payment Gateway" : handles charges
  | uses the LEDGER daily
biz beta "Other" : nothing here
`)
	m := loadClean(t, path)
	cases := map[string]string{
		"PAYMENT":   `alpha [biz] "Payment Gateway" ` + path + ":2",
		"ledger":    `alpha [biz] "Payment Gateway" ` + path + ":2",
		"NOTHING":   `beta [biz] "Other" ` + path + ":4",
		"not-found": "",
	}
	for query, want := range cases {
		got := m.Find(query)
		if want == "" {
			if got != "" {
				t.Errorf("find %q = %q, want no matches", query, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("find %q = %q, want it to contain %q", query, got, want)
		}
	}
}

func TestAreaIsTheFileLocalTopLevelContainer(t *testing.T) {
	root, _, _ := shopFiles(t)
	m := loadClean(t, root)
	if got := m.Area("ml.deploy"); got != "ml" {
		t.Errorf("Area(ml.deploy) = %q, want the mount node ml", got)
	}
	if got := m.Area("signal.ingest"); got != "signal" {
		t.Errorf("Area(signal.ingest) = %q, want the mount node signal", got)
	}
	if got := m.Area("ml"); got != "" {
		t.Errorf("Area(ml) = %q, want no area for a root top-level node", got)
	}

	dir := t.TempDir()
	writeMapFile(t, dir, "deep.smap", `biz deep "Deep"
`)
	writeMapFile(t, dir, "mid.smap", `biz group "Group"
  mount deep.smap
biz other "Other"
`)
	root2 := writeMapFile(t, dir, "root.smap", `core outer "Outer"
  mount mid.smap
`)
	nested := loadClean(t, root2)
	if got := nested.Area("x"); got != "" {
		t.Errorf("Area of an unknown node = %q", got)
	}
	if got := nested.Area("outer.group"); got != "outer" {
		t.Errorf("Area(outer.group) = %q", got)
	}
	if got := nested.Area("outer.group.deep"); got != "outer.group" {
		t.Errorf("Area(outer.group.deep) = %q, want the local top-level node", got)
	}
	if got := nested.RootOf("outer.group.deep"); got != "outer.group.deep" {
		t.Errorf("RootOf(outer.group.deep) = %q, want itself (its file's top level)", got)
	}
}

func TestImpactCrossAreaBizUsesFileLocalAreas(t *testing.T) {
	t.Run("inside one mounted file", func(t *testing.T) {
		dir := t.TempDir()
		writeMapFile(t, dir, "ml.smap", `biz shop "Shop"
  ~ biz checkout "Checkout"
biz billing "Billing"
  biz invoice "Invoice"
checkout -> invoice
`)
		root := writeMapFile(t, dir, "root.smap", `core ml "ML"
  mount ml.smap
`)
		m := loadClean(t, root)
		item := m.Impact().Items[0]
		found := false
		for _, n := range item.Notes {
			if n.Code == "cross-area-biz" && n.Node == "ml.invoice" && n.Area == "ml.billing" {
				found = true
			}
		}
		if !found {
			t.Fatalf("notes = %+v, want cross-area-biz ml.invoice in ml.billing", item.Notes)
		}
	})

	t.Run("across mounted files", func(t *testing.T) {
		dir := t.TempDir()
		writeMapFile(t, dir, "signal.smap", `biz ingest "Ingest"
`)
		writeMapFile(t, dir, "ml.smap", `biz train "Train"
~ biz deploy "Deploy"
ext queue "Queue"
deploy -> queue
deploy -> train
`)
		root := writeMapFile(t, dir, "root.smap", `core ml "ML"
  mount ml.smap
  bind queue = signal.ingest
core signal "Signals"
  mount signal.smap
`)
		m := loadClean(t, root)
		item := m.Impact().Items[0]
		var notes []Note
		for _, n := range item.Notes {
			if n.Code == "cross-area-biz" {
				notes = append(notes, n)
			}
		}
		if len(notes) != 1 || notes[0].Node != "signal.ingest" || notes[0].Area != "signal" {
			t.Fatalf("cross-area notes = %+v, want only signal.ingest in signal", notes)
		}
	})
}
