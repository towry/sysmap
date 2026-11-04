package smap

import (
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
