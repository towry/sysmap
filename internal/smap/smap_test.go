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
	for _, bad := range []string{"http://", "https://", "__DATA__", "__DAGRE__", "__TITLE__", "__LIVE__", "__PELE_SCRIPT__", "__BADGE_ASSETS__", "__DIAG_BADGE__", "__TASK_BADGE__", "__TASK_DETAIL__"} {
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

func TestLoadPicksUpDiagramRefs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := "stateDiagram-v2\n  [*] --> Ready\n"
	retry := "flowchart TD\n  Charge --> Approved\n"
	flow := "flowchart LR\n  Draft --> Paid\n"
	writeMapFile(t, dir, "docs/state.mmd", state)
	writeMapFile(t, dir, "docs/retry.mmd", retry)
	writeMapFile(t, dir, "docs/flow.mermaid", flow)
	root := writeMapFile(t, dir, "maps/system.smap", `system "Diagrams"
biz machine "Machine"
  @ ../docs/state.mmd
  @ ../docs/retry.mmd
biz flow "Flow"
  @ docs/flow.mermaid
`)
	src, m, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if HasErrors(diags) {
		t.Fatalf("diagram refs are not errors: %v", diags)
	}
	got := m.Nodes["machine"].Diagrams
	if len(got) != 2 || got[0].Path != "../docs/state.mmd" || got[0].Text != state ||
		got[1].Path != "../docs/retry.mmd" || got[1].Text != retry {
		t.Fatalf("diagrams on one node = %+v, want the state machine then the retry flowchart", got)
	}
	// The first path is looked up next to maps/, then from the repository root.
	if got := m.Nodes["flow"].Diagrams; len(got) != 1 || got[0].Path != "docs/flow.mermaid" || got[0].Text != flow {
		t.Fatalf("root-fallback diagram = %+v, want %q", got, flow)
	}
	if refs := m.CheckRefs(); len(refs) != 0 {
		t.Fatalf("--refs must not apply the code-ref root rule to diagrams: %+v", refs)
	}

	var v viewData
	if err := json.Unmarshal(m.JSON(src, diags), &v); err != nil {
		t.Fatal(err)
	}
	for _, n := range v.Nodes {
		if n.ID == "machine" && (len(n.Diagrams) != 2 || n.Diagrams[0].Text != state || n.Diagrams[1].Text != retry) {
			t.Fatalf("JSON diagrams = %+v", n.Diagrams)
		}
		if n.ID == "flow" && (len(n.Diagrams) != 1 || n.Diagrams[0].Text != flow) {
			t.Fatalf("JSON diagram = %+v", n.Diagrams)
		}
	}
}

func TestLoadReportsAMissingDiagramOnItsRefLine(t *testing.T) {
	dir := t.TempDir()
	root := writeMapFile(t, dir, "system.smap", `biz checkout "Checkout"
  @ app/orders/checkout.ex:40
  @ docs/missing.mmd
`)
	_, m, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	d := diagWith(diags, "does not exist")
	if d == nil || d.Level != "error" || d.Line != 3 {
		t.Fatalf("missing-diagram diagnostics = %+v, want an error on line 3", diags)
	}
	if !strings.Contains(d.Msg, `diagram "docs/missing.mmd"`) {
		t.Fatalf("message = %s", d.Msg)
	}
	if len(m.Nodes["checkout"].Diagrams) != 0 {
		t.Fatalf("a missing diagram must not be stored: %+v", m.Nodes["checkout"].Diagrams)
	}
}

func TestDiagramRefsMustStayInsideTheRepository(t *testing.T) {
	outside := t.TempDir()
	secret := writeMapFile(t, outside, "secret.mmd", "stateDiagram-v2\n  [*] --> Secret\n")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "maps"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		filepath.ToSlash(secret), // absolute
		"../../outside.mmd",      // escapes the repository
		"https://example.com/secret.mmd",
	}
	linkLine := ""
	// The map-relative candidate is a link outside the repo while the repository-root
	// candidate is simply missing; the containment error must win over "does not exist".
	if err := os.Symlink(secret, filepath.Join(dir, "maps", "leak.mmd")); err == nil {
		linkLine = "biz link \"Link\"\n  @ leak.mmd\n"
	}
	src := "biz absolute \"Absolute\"\n  @ " + cases[0] + "\nbiz escape \"Escape\"\n  @ " + cases[1] + "\nbiz url \"URL\"\n  @ " + cases[2] + "\n" + linkLine
	root := writeMapFile(t, dir, "maps/system.smap", src)
	_, m, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := len(cases)
	if linkLine != "" {
		want++
	}
	found := 0
	for _, d := range diags {
		if d.Level == "error" && strings.Contains(d.Msg, "must be relative to the .smap file and inside") {
			found++
		}
	}
	if found != want {
		t.Fatalf("path containment errors = %d, want %d: %+v", found, want, diags)
	}
	for _, id := range m.Order {
		if len(m.Nodes[id].Diagrams) != 0 {
			t.Fatalf("refused diagram was loaded on %s: %+v", id, m.Nodes[id].Diagrams)
		}
	}
}

func TestHTMLEmbedsTheDiagramRendererOnlyWhenNeeded(t *testing.T) {
	dir := t.TempDir()
	writeMapFile(t, dir, "docs/state.mmd", "stateDiagram-v2\n  [*] --> Ready\n")
	root := writeMapFile(t, dir, "system.smap", `core platform "Platform"
biz machine "Machine"
  @ docs/state.mmd
`)
	src, m, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	page := string(m.HTML(src, diags, false))
	if !strings.Contains(page, "var pele=") {
		t.Fatal("a map with a diagram must embed the renderer")
	}
	if !strings.Contains(page, "Pele 1.13.0, MIT License") || !strings.Contains(page, "Copyright (c) 2026 kepano") {
		t.Fatal("the Pele licence notice must travel with the renderer")
	}
	if strings.Contains(page, "__PELE_SCRIPT__") {
		t.Fatal("the renderer placeholder must be replaced")
	}
	// The only URL schemes left are SVG namespace identifiers and the panzoom bundle's
	// repository comment, which are not requests. The vendored Pele banner and licence
	// are stripped of schemes when embedded.
	clean := strings.NewReplacer(
		"https://github.com/timmywil/panzoom", "",
		"http://www.w3.org/2000/svg", "",
		"http://www.w3.org/1999/xhtml", "",
	).Replace(page)
	if strings.Contains(clean, "http://") || strings.Contains(clean, "https://") {
		t.Fatal("a built page with a diagram must not contain a network URL")
	}

	plain := string(mustParse(t, shop).HTML("src", nil, false))
	if strings.Contains(plain, "var pele=") || strings.Contains(plain, "Pele 1.13.0") {
		t.Fatal("a map without diagrams must not embed the renderer")
	}
	if strings.Contains(plain, "__PELE_SCRIPT__") {
		t.Fatal("the renderer placeholder must be replaced on every page")
	}
}

func TestVersionHashCoversDiagramFiles(t *testing.T) {
	dir := t.TempDir()
	diagram := writeMapFile(t, dir, "docs/state.mmd", "stateDiagram-v2\n  [*] --> Ready\n")
	root := writeMapFile(t, dir, "system.smap", `biz machine "Machine"
  @ docs/state.mmd
`)
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
	if err := os.WriteFile(diagram, []byte("stateDiagram-v2\n  [*] --> Ready\n  Ready --> Done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := version()
	if before == after {
		t.Fatalf("version %q did not change when the diagram changed", before)
	}
}

func TestImpactExistingNodeWiredToNewNodeIsNotAnExtension(t *testing.T) {
	m := mustParse(t, "core auth\nbiz orders\n  biz checkout\nbiz web\nweb -> checkout\ncheckout -> auth\n+ biz promo\npromo -> auth\ncheckout -> promo\n")
	imp := m.Impact()
	if imp.Level != "contained" || strings.Join(imp.Summary.Wired, ",") != "checkout" || strings.Join(imp.Summary.Inspect, ",") != "web" || strings.Join(imp.Summary.Reused, ",") != "auth" || strings.Join(imp.Affected, ",") != "checkout,web" {
		t.Fatalf("existing nodes that point at a new node must change too: %+v", imp)
	}
	if strings.Contains(imp.Verdict, "changes no existing node") || !strings.Contains(imp.Verdict, "existing node(s) checkout depend on the new node(s) and must change too") || !strings.Contains(imp.Verdict, "inspect existing dependent(s) web") {
		t.Fatalf("verdict = %s", imp.Verdict)
	}
	if txt := imp.Text(); !strings.Contains(txt, "wired dependents (must change too, 1):\n    checkout\n") || !strings.Contains(txt, "existing dependents to inspect (1):\n    web (via checkout)\n") {
		t.Fatalf("text =\n%s", txt)
	}
}

func TestImpactEdgeToContainerReachesItsChangedChild(t *testing.T) {
	m := mustParse(t, "biz web\nbiz orders\n  ~ biz checkout\nweb -> orders\n")
	imp := m.Impact()
	if got := strings.Join(imp.Affected, ","); got != "web" {
		t.Fatalf("an edge to the container uses its children; affected = %q", got)
	}
	if len(imp.Summary.Wired) != 0 || strings.Join(imp.Summary.Inspect, ",") != "web" {
		t.Fatalf("container edge to ~ child must be inspect, not wired: %+v", imp.Summary)
	}
	if got := strings.Join(imp.Items[0].Paths["web"], ">"); got != "web>orders>checkout" {
		t.Fatalf("container must appear in the path: %q", got)
	}
	if txt := imp.Text(); !strings.Contains(txt, "web (via orders)") {
		t.Fatalf("text lacks container hop:\n%s", txt)
	}

	// A new (+) child inside a container is not wired to callers of the container.
	ext := mustParse(t, "core auth\nbiz web\nbiz orders\n  + biz promo\npromo -> auth\nweb -> orders\n").Impact()
	if ext.Level != "extension" || len(ext.Affected) != 0 || len(ext.Summary.Wired) != 0 {
		t.Fatalf("new child inside a container must stay an extension: %+v", ext)
	}

	// When ~ report calls + gate inside a container that shipper points to, shipper is inspected
	// once via the container on ~ report and never marked wired to + gate.
	plan := mustParse(t, "core control\nbiz shipper\nbiz billing\n  ~ biz report\n  + biz gate\nreport -> control\nreport -> gate\nshipper -> billing\n").Impact()
	if plan.Level != "contained" || len(plan.Summary.Wired) != 0 || strings.Join(plan.Summary.Inspect, ",") != "shipper" || strings.Join(plan.Summary.Reused, ",") != "control" {
		t.Fatalf("combined container + planned caller summary = %+v", plan.Summary)
	}
	if strings.Contains(plan.Verdict, "must change too") || !strings.Contains(plan.Verdict, "reuses shared node(s) control (unchanged)") || !strings.Contains(plan.Verdict, "inspect existing dependent(s) shipper") {
		t.Fatalf("verdict = %s", plan.Verdict)
	}
	if len(plan.Items[1].Dependents) != 0 {
		t.Fatalf("+ gate must not duplicate dependents through ~ report: %+v", plan.Items[1])
	}

	// Direct edge wins over container edge regardless of declaration order.
	for _, edges := range []string{"web -> orders\nweb -> checkout\n", "web -> checkout\nweb -> orders\n"} {
		both := mustParse(t, "biz web\nbiz orders\n  ~ biz checkout\n"+edges).Impact()
		if got := strings.Join(both.Items[0].Paths["web"], ">"); got != "web>checkout" {
			t.Fatalf("edges %q gave path %q, want direct web>checkout", edges, got)
		}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
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

// --- check --refs ---

func TestCheckRefsFindsMissingFilesAndLinesPastTheEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMapFile(t, dir, "app/a.go", "one\ntwo\nthree\n")
	writeMapFile(t, dir, "app/nonl.txt", "one\ntwo")
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := writeMapFile(t, dir, "maps/root.smap", `system "Refs"
biz a "A"
  @ app/a.go:3
  @ app/a.go:4
  @ app/missing.go
  @ docs
  @ docs:99
  @ app/nonl.txt:2
  @ app/nonl.txt:3
biz b "B"
  @ app/a.go
`)
	_, m, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if HasErrors(diags) {
		t.Fatalf("refs must be errors only with --refs: %v", diags)
	}
	refDiags := m.CheckRefs()
	if len(refDiags) != 3 {
		t.Fatalf("ref diagnostics = %+v", refDiags)
	}
	want := map[int]string{
		4: "app/a.go has only 3 lines",
		5: "app/missing.go does not exist",
		9: "app/nonl.txt has only 2 lines",
	}
	for line, text := range want {
		found := false
		for _, d := range refDiags {
			if d.Level == "error" && d.File == root && d.Line == line && strings.Contains(d.Msg, text) {
				found = true
			}
		}
		if !found {
			t.Errorf("line %d: no diagnostic containing %q in %+v", line, text, refDiags)
		}
	}
}

func TestCheckRefsResolvesMountedFilesFromTheRepositoryRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMapFile(t, dir, "app/a.go", "one\ntwo\n")
	child := writeMapFile(t, dir, "maps/child.smap", `biz a "A"
  @ app/a.go:2
  @ app/a.go:9
`)
	root := writeMapFile(t, dir, "maps/root.smap", `core c "C"
  mount child.smap
`)
	_, m, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := m.CheckRefs()
	if len(diags) != 1 || diags[0].File != child || diags[0].Line != 3 {
		t.Fatalf("mounted ref diagnostics = %+v, want the bad ref at %s:3", diags, child)
	}
	if !strings.Contains(diags[0].Msg, "app/a.go has only 2 lines") {
		t.Fatalf("message = %s", diags[0].Msg)
	}

	// A mounted file checked alone resolves from the same repository root, not maps/.
	_, alone, _, err := Load(child)
	if err != nil {
		t.Fatal(err)
	}
	aloneDiags := alone.CheckRefs()
	if len(aloneDiags) != 1 || aloneDiags[0].Line != 3 {
		t.Fatalf("ref diagnostics for the file alone = %+v", aloneDiags)
	}
}

func TestCheckRefsFallsBackToTheMapDirectoryWithoutGit(t *testing.T) {
	dir := t.TempDir()
	writeMapFile(t, dir, "maps/data.txt", "one\ntwo\n")
	root := writeMapFile(t, dir, "maps/plain.smap", `biz a "A"
  @ data.txt:2
  @ data.txt:3
`)
	_, m, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	diags := m.CheckRefs()
	if len(diags) != 1 || diags[0].Line != 3 {
		t.Fatalf("ref diagnostics = %+v, want only the line past the end", diags)
	}
	if !strings.Contains(diags[0].Msg, "data.txt has only 2 lines") {
		t.Fatalf("message = %s", diags[0].Msg)
	}
}

// --- containment and dependency-cycle warnings ---

func cycleWarnings(diags []Diag) []Diag {
	var out []Diag
	for _, d := range diags {
		if d.Level == "warn" && strings.Contains(d.Msg, "dependency cycle") {
			out = append(out, d)
		}
	}
	return out
}

func containmentWarnings(diags []Diag) []Diag {
	var out []Diag
	for _, d := range diags {
		if d.Level == "warn" && strings.Contains(d.Msg, "sits inside shared node") {
			out = append(out, d)
		}
	}
	return out
}

const cycleHint = `; confirm it with a "| cycle: <reason>" detail line on one of them or break it`

func TestDependencyCycleWarnsOncePerGroup(t *testing.T) {
	cases := []struct {
		name, src, between string
	}{
		{
			name:    "two nodes",
			src:     "biz a\nbiz b\na -> b\nb -> a\n",
			between: "a and b",
		},
		{
			name:    "three nodes",
			src:     "biz a\nbiz b\nbiz c\na -> b\nb -> c\nc -> a\n",
			between: "a, b and c",
		},
		{
			// Five edges but one group: the warning must not repeat per edge.
			name:    "bigger group",
			src:     "biz a\nbiz b\nbiz c\nbiz d\na -> b\nb -> c\nc -> d\nd -> a\na -> c\n",
			between: "a, b, c and d",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mustParse(t, tc.src)
			got := cycleWarnings(m.Check())
			if len(got) != 1 {
				t.Fatalf("cycle warnings = %+v, want exactly one group", got)
			}
			want := "dependency cycle between " + tc.between + cycleHint
			if got[0].Msg != want {
				t.Fatalf("message = %q, want %q", got[0].Msg, want)
			}
		})
	}
}

func TestCycleDetailConfirmsOnlyWithTextOnACycleNode(t *testing.T) {
	base := func(detail string) string {
		return "biz a\nbiz b\n  | " + detail + "\na -> b\nb -> a\n"
	}
	cases := []struct {
		name     string
		src      string
		wantWarn bool
	}{
		{"text confirms", base("cycle: both directions are intentional"), false},
		{"empty does not confirm", base("cycle:"), true},
		{"spaces do not confirm", base("cycle:   "), true},
		{
			name:     "text on an unrelated node does not confirm",
			src:      "biz a\nbiz b\nbiz c\n  | cycle: unrelated\nbiz d\na -> b\nb -> a\n",
			wantWarn: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mustParse(t, tc.src)
			got := cycleWarnings(m.Check())
			if tc.wantWarn && len(got) != 1 {
				t.Fatalf("cycle warnings = %+v, want one", got)
			}
			if !tc.wantWarn && len(got) != 0 {
				t.Fatalf("cycle warnings = %+v, want none for a confirmed cycle", got)
			}
		})
	}
}

func TestTwoCyclesWarnSeparately(t *testing.T) {
	m := mustParse(t, "biz a\nbiz b\nbiz c\nbiz d\na -> b\nb -> a\nc -> d\nd -> c\n")
	got := cycleWarnings(m.Check())
	if len(got) != 2 {
		t.Fatalf("cycle warnings = %+v, want two groups", got)
	}
	messages := got[0].Msg + " | " + got[1].Msg
	for _, want := range []string{
		"dependency cycle between a and b" + cycleHint,
		"dependency cycle between c and d" + cycleHint,
	} {
		if !strings.Contains(messages, want) {
			t.Fatalf("messages = %q, missing %q", messages, want)
		}
	}
}

func TestAcyclicDiamondHasNoCycleWarning(t *testing.T) {
	m := mustParse(t, "biz a\nbiz b\nbiz c\nbiz d\na -> b\na -> c\nb -> d\nc -> d\n")
	if got := cycleWarnings(m.Check()); len(got) != 0 {
		t.Fatalf("cycle warnings = %+v, want none", got)
	}
}

func TestCycleCheckIgnoresSelfLoopsAndContainmentEdges(t *testing.T) {
	t.Run("self loop", func(t *testing.T) {
		m, _ := Parse("biz a\nbiz b\na -> a\na -> b\nb -> a\n")
		diags := m.Check()
		if got := cycleWarnings(diags); len(got) != 1 {
			t.Fatalf("cycle warnings = %+v, want only the a/b group", got)
		}
		errs := 0
		for _, d := range diags {
			if d.Level == "error" && strings.Contains(d.Msg, "itself") {
				errs++
			}
		}
		if errs != 1 {
			t.Fatalf("self-loop errors = %d, want 1 in %+v", errs, diags)
		}
	})

	t.Run("container and child", func(t *testing.T) {
		m := mustParse(t, "biz parent\n  biz child\nparent -> child\nchild -> parent\n")
		diags := m.Check()
		if got := cycleWarnings(diags); len(got) != 0 {
			t.Fatalf("cycle warnings = %+v, want none for container edges", got)
		}
		contain := 0
		for _, d := range diags {
			if strings.Contains(d.Msg, "contain one another") {
				contain++
			}
		}
		if contain != 2 {
			t.Fatalf("containment warnings = %d, want 2 in %+v", contain, diags)
		}
	})
}

func TestSingleFileCycleCarriesItsLine(t *testing.T) {
	dir := t.TempDir()
	path := writeMapFile(t, dir, "cycle.smap", "biz a \"A\"\nbiz b \"B\"\na -> b\nb -> a\n")
	_, m, diags, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cycleWarnings(diags)
	if len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("cycle warnings = %+v, want one at line 3", got)
	}
	if got := cycleWarnings(m.Check()); len(got) != 1 {
		t.Fatalf("Check on the loaded model = %+v", got)
	}
}

func TestCycleAcrossTwoMountedFiles(t *testing.T) {
	dir := t.TempDir()
	one := writeMapFile(t, dir, "one.smap", `biz a "A"
ext sa "Stub"
a -> sa
`)
	writeMapFile(t, dir, "two.smap", `biz b "B"
ext sb "Stub"
b -> sb
`)
	root := writeMapFile(t, dir, "root.smap", `core one "One"
  mount one.smap
  bind sa = two.b
core two "Two"
  mount two.smap
  bind sb = one.a
`)
	_, m, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := containmentWarnings(diags); len(got) != 0 {
		t.Fatalf("mounted top-level biz nodes must be exempt: %+v", got)
	}
	got := cycleWarnings(diags)
	if len(got) != 1 {
		t.Fatalf("cycle warnings = %+v, want one", got)
	}
	want := "dependency cycle between one.a and two.b" + cycleHint
	if got[0].Msg != want {
		t.Fatalf("message = %q, want %q", got[0].Msg, want)
	}
	if got[0].File != one || got[0].Line != 3 {
		t.Fatalf("warning at %s:%d, want %s:3", got[0].File, got[0].Line, one)
	}
	if got := cycleWarnings(m.Check()); len(got) != 1 {
		t.Fatalf("Check on the composed model = %+v", got)
	}
}

func TestCycleCreatedOnlyByBinds(t *testing.T) {
	dir := t.TempDir()
	one := writeMapFile(t, dir, "one.smap", `biz a "A"
biz c "C"
ext sa "SA"
ext sc "SC"
a -> sa
c -> sc
`)
	writeMapFile(t, dir, "two.smap", `biz b "B"
biz d "D"
ext sb "SB"
ext sd "SD"
b -> sb
d -> sd
`)
	root := writeMapFile(t, dir, "root.smap", `core one "One"
  mount one.smap
  bind sa = two.b
  bind sc = two.d
core two "Two"
  mount two.smap
  bind sb = one.c
  bind sd = one.a
`)
	_, _, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got := cycleWarnings(diags)
	if len(got) != 1 {
		t.Fatalf("cycle warnings = %+v, want one group created by binds", got)
	}
	want := "dependency cycle between one.a, one.c, two.b and two.d" + cycleHint
	if got[0].Msg != want {
		t.Fatalf("message = %q, want %q", got[0].Msg, want)
	}
	if got[0].File != one || got[0].Line != 5 {
		t.Fatalf("warning at %s:%d, want %s:5", got[0].File, got[0].Line, one)
	}
}

func TestCycleDetailConfirmsAfterComposition(t *testing.T) {
	dir := t.TempDir()
	writeMapFile(t, dir, "one.smap", `biz a "A"
  | cycle: the two sides must call each other
ext sa "Stub"
a -> sa
`)
	writeMapFile(t, dir, "two.smap", `biz b "B"
ext sb "Stub"
b -> sb
`)
	root := writeMapFile(t, dir, "root.smap", `core one "One"
  mount one.smap
  bind sa = two.b
core two "Two"
  mount two.smap
  bind sb = one.a
`)
	_, _, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cycleWarnings(diags); len(got) != 0 {
		t.Fatalf("confirmed cycle still warns: %+v", got)
	}
}

func TestBusinessNodeInsideSharedNodeWarns(t *testing.T) {
	cases := []struct {
		name, src string
		want      int
	}{
		{"biz under core", "core shared \"S\"\n  biz app \"A\"\n", 1},
		{"biz under biz", "biz parent \"P\"\n  biz child \"C\"\n", 0},
		{"core under biz", "biz parent \"P\"\n  core child \"C\"\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeMapFile(t, dir, "root.smap", tc.src)
			_, _, diags, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			got := containmentWarnings(diags)
			if len(got) != tc.want {
				t.Fatalf("containment warnings = %+v, want %d", got, tc.want)
			}
			if tc.want == 0 {
				return
			}
			want := `business node "app" sits inside shared node "shared"; shared nodes should not contain business code`
			if got[0].Msg != want || got[0].Line != 2 {
				t.Fatalf("warning = %+v, want %q at line 2", got[0], want)
			}
			if got := containmentWarnings(mustParse(t, tc.src).Check()); len(got) != tc.want {
				t.Fatalf("Check on the parsed model = %+v", got)
			}
		})
	}
}

func TestContainmentIsCheckedInsideMountedFiles(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "child.smap", "core shared \"S\"\n  biz app \"A\"\n")
	root := writeMapFile(t, dir, "root.smap", `core ml "ML"
  mount child.smap
`)
	_, _, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got := containmentWarnings(diags)
	if len(got) != 1 || got[0].File != child || got[0].Line != 2 {
		t.Fatalf("containment warnings = %+v, want one at %s:2", got, child)
	}
	if !strings.Contains(got[0].Msg, `"app"`) || !strings.Contains(got[0].Msg, `"shared"`) {
		t.Fatalf("message = %s", got[0].Msg)
	}

	// Checking the mounted file alone must report the same local problem.
	_, _, alone, err := Load(child)
	if err != nil {
		t.Fatal(err)
	}
	if got := containmentWarnings(alone); len(got) != 1 {
		t.Fatalf("containment warnings for the file alone = %+v", got)
	}
}

func TestMountBoundaryDoesNotJudgeMountedTopLevelNodes(t *testing.T) {
	dir := t.TempDir()
	writeMapFile(t, dir, "child.smap", "biz deploy \"Deploy\"\n")
	root := writeMapFile(t, dir, "root.smap", `core ml "ML"
  mount child.smap
  biz manual "Manual"
`)
	_, _, diags, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got := containmentWarnings(diags)
	if len(got) != 1 {
		t.Fatalf("containment warnings = %+v, want only the parent-file child", got)
	}
	if !strings.Contains(got[0].Msg, `"manual"`) || strings.Contains(got[0].Msg, `"deploy"`) {
		t.Fatalf("warning = %s, want only manual", got[0].Msg)
	}

	// A top-level biz node in a mounted file has no parent inside its own file.
	_, _, alone, err := Load(filepath.Join(dir, "child.smap"))
	if err != nil {
		t.Fatal(err)
	}
	if got := containmentWarnings(alone); len(got) != 0 {
		t.Fatalf("mounted top-level biz warned on its own: %+v", got)
	}
}

func TestSpecLeavesExamplesAtColumnZero(t *testing.T) {
	want := "Top-level lines start in column 0; children are indented two spaces."
	if !strings.Contains(Spec, want) {
		t.Fatalf("spec lacks the indentation sentence %q", want)
	}
	for _, line := range []string{
		`system "Name" : one line on what the system is`,
		`core platform "Platform" : shared by many business nodes; change with care`,
		`biz billing "Billing" : business-specific`,
		`ext stripe "Stripe" : outside the system`,
		`invoice -> ledger : post entry      (-> call, ~> async)`,
		`flow checkout "Checkout" : optional note`,
		`core ml "ML pipeline" : research to the daily run`,
		`ext platform_pg "Postgres" : shared database`,
		`+ biz refund "Refund" : new node, not built yet`,
		`~ core ledger "Ledger" : existing node that the change will modify`,
	} {
		if !strings.Contains(Spec, "\n"+line+"\n") {
			t.Errorf("top-level example is not at column 0: %q", line)
		}
		if strings.Contains(Spec, "\n  "+line+"\n") {
			t.Errorf("top-level example still has the display indent: %q", line)
		}
	}
	for _, line := range []string{
		`  core ledger "Ledger" : the only door for money movement`,
		`    | extra lines of detail, shown when you zoom in`,
		`  biz invoice "Invoice" : only it may mark an order invoiced`,
		`  cart -> invoice : submit order    (steps follow the edges above)`,
		`  mount ../docs/architecture/ml.smap`,
		`  bind postgres = platform_pg`,
	} {
		if !strings.Contains(Spec, "\n"+line+"\n") {
			t.Errorf("relative example indentation changed: %q", line)
		}
	}
	for _, block := range []string{
		"\nbiz a \"A\"\n  | cycle: both directions are required during the migration\n",
		"\nbiz a \"A\"\n  | unverified: no load test yet\n",
	} {
		if !strings.Contains(Spec, block) {
			t.Errorf("example block is not at column 0 with a two-space detail: %q", block)
		}
	}
}

func TestIndentAndFlowDiagnosticsExplainTheFix(t *testing.T) {
	_, diags := Parse("core a\n    core b\n")
	d := diagWith(diags, "indent jumps a level")
	if d == nil || !strings.Contains(d.Msg, "top-level lines start in column 0") || !strings.Contains(d.Msg, "two spaces") {
		t.Fatalf("indent diagnostic = %+v", diags)
	}

	_, diags = Parse("flow f\n  core a\n")
	d = diagWith(diags, "inside a flow only steps")
	if d == nil || !strings.Contains(d.Msg, "an indented line after") || !strings.Contains(d.Msg, "line in column 0 ends the flow") {
		t.Fatalf("flow diagnostic = %+v", diags)
	}
}

func TestFindShowsTheFirstMatchingText(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 80) + " NEEDLE " + strings.Repeat("y", 80)
	path := writeMapFile(t, dir, "find.smap", "system \"Find\"\nbiz alpha \"Payment Gateway\" : handles charges\n  | uses the LEDGER daily\nbiz beta \"Other\" : nothing here\nbiz gamma \"Gamma\" : "+long+"\n")
	m := loadClean(t, path)

	cases := []struct {
		query, want, absent string
	}{
		{"charges", "  desc: handles charges", ""},
		{"ledger", "  | uses the LEDGER daily", ""},
		{"gateway", "  title: Payment Gateway", ""},
		{"alpha", "", "  desc:"},
		{"beta", "", "  title:"},
	}
	for _, tc := range cases {
		got := m.Find(tc.query)
		if tc.query == "beta" {
			if !strings.Contains(got, "beta [biz] \"Other\"") {
				t.Errorf("find %q = %q, want the beta hit", tc.query, got)
			}
			for _, bad := range []string{"  desc:", "  | ", "  title:"} {
				if strings.Contains(got, bad) {
					t.Errorf("find %q = %q, must not have a snippet", tc.query, got)
				}
			}
			continue
		}
		if !strings.Contains(got, "alpha [biz] \"Payment Gateway\"") {
			t.Errorf("find %q = %q, want the alpha hit", tc.query, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("find %q = %q, want snippet %q", tc.query, got, tc.want)
		}
		if tc.absent != "" && strings.Contains(got, tc.absent) {
			t.Errorf("find %q = %q, must not contain %q", tc.query, got, tc.absent)
		}
	}

	got := m.Find("needle")
	if !strings.Contains(got, "  desc: ") || !strings.Contains(got, "NEEDLE") {
		t.Fatalf("long description snippet = %q", got)
	}
	line := strings.SplitN(got, "\n", 3)[1]
	snippet := strings.TrimPrefix(line, "  desc: ")
	if n := len([]rune(snippet)); n > 100 {
		t.Fatalf("snippet has %d runes, want at most 100: %q", n, snippet)
	}
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") {
		t.Fatalf("clipped snippet should mark both cuts: %q", snippet)
	}
}

func TestShowFlowPrintsTitleDescriptionAndSteps(t *testing.T) {
	dir := t.TempDir()
	path := writeMapFile(t, dir, "flow.smap", `system "S"
biz a "A"
biz b "B"
a -> b
flow daily "Daily" : every day
  a -> b : go
`)
	m := loadClean(t, path)
	got, err := m.Show("daily")
	if err != nil {
		t.Fatal(err)
	}
	want := "flow daily \"Daily\" (" + path + ":5)\n" +
		"every day\n" +
		"steps:\n" +
		"  a -> b : go (" + path + ":6)\n"
	if got != want {
		t.Fatalf("show daily = %q, want %q", got, want)
	}
}

func TestShowFlowWithoutDescriptionOrNote(t *testing.T) {
	dir := t.TempDir()
	path := writeMapFile(t, dir, "flow.smap", `system "S"
biz a "A"
biz b "B"
a -> b
flow quiet "Quiet"
  a -> b
`)
	m := loadClean(t, path)
	got, err := m.Show("quiet")
	if err != nil {
		t.Fatal(err)
	}
	want := "flow quiet \"Quiet\" (" + path + ":5)\nsteps:\n  a -> b (" + path + ":6)\n"
	if got != want {
		t.Fatalf("show quiet = %q, want %q", got, want)
	}
}

func TestShowQualifiedFlowFromMountedFile(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "ml.smap", `system "ML"
biz a "A"
biz b "B"
a -> b
flow daily "Daily" : every day
  a -> b : go
`)
	root := writeMapFile(t, dir, "root.smap", `core ml "ML"
  mount ml.smap
`)
	m := loadClean(t, root)
	got, err := m.Show("ml.daily")
	if err != nil {
		t.Fatal(err)
	}
	want := "flow ml.daily \"Daily\" (" + child + ":5)\n" +
		"every day\n" +
		"steps:\n" +
		"  ml.a -> ml.b : go (" + child + ":6)\n"
	if got != want {
		t.Fatalf("show ml.daily = %q, want %q", got, want)
	}
}

func TestShowNodeWinsOverAFlowWithTheSameId(t *testing.T) {
	m := mustParse(t, "biz daily \"Node\"\nbiz other \"Other\"\ndaily -> other\nflow daily \"Daily flow\" : f\n  daily -> other : go\n")
	got, err := m.Show("daily")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "[biz] daily \"Node\"\n") {
		t.Fatalf("show daily did not print the node:\n%s", got)
	}
	if !strings.HasSuffix(got, "note: a flow with the same id also exists\n") {
		t.Fatalf("show daily lacks the flow note:\n%s", got)
	}
}

func TestShowUnknownIdMentionsNodesAndFlows(t *testing.T) {
	m := mustParse(t, "biz a \"A\"\n")
	_, err := m.Show("ghost")
	if err == nil || !strings.Contains(err.Error(), "neither a node nor a flow") {
		t.Fatalf("unknown id error = %v", err)
	}
}

func TestCaveatLinesAndOutlineMarker(t *testing.T) {
	dir := t.TempDir()
	path := writeMapFile(t, dir, "caveats.smap", `system "S"
core dep "D"
biz a "A" : has caveats
  | unverified: no load test
  | open: who owns it?
  | detail without prefix
dep -> a
biz b "B"
  | unverified:
  | open:
  | Unverified: uppercase is not a caveat
`)
	if got := caveatLines(&Node{Detail: []string{"open:   ", "unverified:\t"}}); len(got) != 0 {
		t.Fatalf("whitespace-only caveats counted: %v", got)
	}
	m, diags := loadModel(t, path)
	if HasErrors(diags) {
		t.Fatalf("load diagnostics: %v", diags)
	}
	want := "a [biz] \"A\" " + path + ":3\n  unverified: no load test\n  open: who owns it?\n"
	if got := m.FindCaveats(); got != want {
		t.Fatalf("find --caveats = %q, want %q", got, want)
	}
	out, err := m.Outline("", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `[biz] a "A" ←1 ?2 — has caveats`) {
		t.Fatalf("outline lacks the caveat marker:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, `[biz] b "B"`) && strings.Contains(line, "?") {
			t.Fatalf("empty or uppercase caveat was counted: %s", line)
		}
	}
}

func TestCaveatsInMountedFile(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "ml.smap", `system "ML"
biz deploy "Deploy" : ships models
  | unverified: not load tested
  | open: rollout owner?
`)
	root := writeMapFile(t, dir, "root.smap", `core ml "ML"
  mount ml.smap
`)
	m := loadClean(t, root)
	want := "ml.deploy [biz] \"Deploy\" " + child + ":2\n  unverified: not load tested\n  open: rollout owner?\n"
	if got := m.FindCaveats(); got != want {
		t.Fatalf("find --caveats = %q, want %q", got, want)
	}
	out, err := m.Outline("ml", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `[biz] ml.deploy "Deploy" ?2 — ships models`) {
		t.Fatalf("outline lacks the mounted caveat marker:\n%s", out)
	}
}

func TestSpecDocumentsFlowsAndCaveats(t *testing.T) {
	for _, want := range []string{"unverified:", "open:", "find --caveats", "show <flow>"} {
		if !strings.Contains(Spec, want) {
			t.Errorf("spec lacks %q", want)
		}
	}
}

func TestFileVersionsInMountedMaps(t *testing.T) {
	dir := t.TempDir()
	child := writeMapFile(t, dir, "child.smap", "# sysmap-format: 1\n# sysmap-reviewed-with: v0.1.1\nbiz child\n")
	root := writeMapFile(t, dir, "root.smap", "biz parent\n  mount child.smap\n")
	m := loadClean(t, root)
	if len(m.FileVersions) != 2 || m.FileVersions[0].File != root || m.FileVersions[0].Format != "" || m.FileVersions[1].File != child || m.FileVersions[1].Reviewed != "v0.1.1" {
		t.Fatalf("file versions: %+v", m.FileVersions)
	}
	old := ProgramVersion
	t.Cleanup(func() { ProgramVersion = old })
	ProgramVersion = "v0.1.2"
	if !strings.Contains(m.VersionText(), "v0.1.1 to v0.1.2") {
		t.Fatal(m.VersionText())
	}
	ProgramVersion = "dev"
	if !strings.Contains(m.VersionText(), "not a stable release") {
		t.Fatal(m.VersionText())
	}
	ProgramVersion = "v0.1.2"
	m.FileVersions[1].Reviewed = "v0.10.0"
	if !strings.Contains(m.VersionText(), "newer Sysmap release") {
		t.Fatal(m.VersionText())
	}
	m.FileVersions[1].Reviewed = "v0.1.1"
	m.FileVersions[1].File = "dir/map's file.smap"
	if !strings.Contains(m.VersionText(), "sysmap check --refs 'dir/map'\\''s file.smap'") {
		t.Fatal(m.VersionText())
	}
	m.FileVersions[1].Format = ""
	if got := m.VersionText(); !strings.Contains(got, "format is undeclared") || !strings.Contains(got, "v0.1.1 to v0.1.2") {
		t.Fatal(got)
	}
}

func TestInvalidFileVersionDeclarations(t *testing.T) {
	for _, source := range []string{
		"# sysmap-format: 2\nbiz a\n",
		"# sysmap-format: 1\n# sysmap-format: 1\nbiz a\n",
		"biz a\n# sysmap-format: 1\n",
		"# sysmap-reviewed-with: v1.2.3-rc1\nbiz a\n",
		"#sysmap-format: 2\nbiz a\n",
	} {
		_, diags := Parse(source)
		if !HasErrors(diags) {
			t.Errorf("expected error for %q", source)
		}
	}
}
