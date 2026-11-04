package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/towry/sysmap/internal/smap"
)

func TestCheckRefsOnlyFailsWithTheFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "root.smap")
	if err := os.WriteFile(path, []byte("biz a \"A\"\n  @ a.txt:9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"check", path}); code != 0 {
		t.Fatalf("check without --refs = %d, want 0", code)
	}
	if code := run([]string{"check", "--refs", path}); code != 1 {
		t.Fatalf("check --refs = %d, want 1", code)
	}
}

func TestCheckReportsAMissingDiagramWithItsLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "root.smap")
	if err := os.WriteFile(path, []byte("biz a \"A\"\n  @ docs/state.mmd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := captureRun(t, "check", path)
	if code != 1 || out != "" {
		t.Fatalf("check = exit %d, stdout %q", code, out)
	}
	if !strings.Contains(errOut, path+":2: error:") || !strings.Contains(errOut, `diagram "docs/state.mmd" does not exist`) {
		t.Fatalf("check stderr = %q", errOut)
	}
}

func captureRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	code := run(args)
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, err := io.ReadAll(rOut)
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := io.ReadAll(rErr)
	if err != nil {
		t.Fatal(err)
	}
	rOut.Close()
	rErr.Close()
	return code, string(out), string(errOut)
}

func TestDiagnosticLinesQuietsWarningsOutsideCheck(t *testing.T) {
	diags := []smap.Diag{
		{File: "/tmp/a.smap", Line: 2, Level: "warn", Msg: "warning one"},
		{File: "/tmp/a.smap", Line: 3, Level: "error", Msg: "error one"},
	}
	checkLines := diagnosticLines("/tmp/a.smap", true, diags)
	wantCheck := []string{
		"/tmp/a.smap:2: warn: warning one",
		"/tmp/a.smap:3: error: error one",
	}
	if strings.Join(checkLines, "\n") != strings.Join(wantCheck, "\n") {
		t.Fatalf("check lines = %q, want %q", checkLines, wantCheck)
	}

	quiet := diagnosticLines("/tmp/a.smap", false, diags)
	wantQuiet := []string{
		"/tmp/a.smap:3: error: error one",
		"note: 1 warning (run 'sysmap check' to see them)",
	}
	if strings.Join(quiet, "\n") != strings.Join(wantQuiet, "\n") {
		t.Fatalf("non-check lines = %q, want %q", quiet, wantQuiet)
	}

	two := diagnosticLines("/tmp/a.smap", false, []smap.Diag{
		{File: "/tmp/a.smap", Line: 1, Level: "warn", Msg: "a"},
		{File: "/tmp/a.smap", Line: 2, Level: "warn", Msg: "b"},
	})
	if len(two) != 1 || two[0] != "note: 2 warnings (run 'sysmap check' to see them)" {
		t.Fatalf("two-warning summary = %q", two)
	}

	if got := diagnosticLines("/tmp/a.smap", false, nil); len(got) != 0 {
		t.Fatalf("no diagnostics = %q, want no lines", got)
	}

	three := []smap.Diag{
		{File: "/tmp/a.smap", Line: 1, Level: "error", Msg: "same"},
		{File: "/tmp/a.smap", Line: 2, Level: "error", Msg: "same"},
		{File: "/tmp/a.smap", Line: 3, Level: "error", Msg: "same"},
	}
	if got := diagnosticLines("/tmp/a.smap", true, three); len(got) != 3 || strings.Contains(strings.Join(got, "\n"), "more like this") {
		t.Fatalf("three identical diagnostics must not collapse: %q", got)
	}
	four := append(append([]smap.Diag(nil), three...), smap.Diag{File: "/tmp/a.smap", Line: 4, Level: "error", Msg: "same"})
	if got := diagnosticLines("/tmp/a.smap", true, four); !strings.Contains(strings.Join(got, "\n"), "(and 1 more like this in /tmp/a.smap)") {
		t.Fatalf("four identical diagnostics must collapse: %q", got)
	}
}

func TestDiagnosticLinesCollapseRepeatedMessagesPerFile(t *testing.T) {
	var diags []smap.Diag
	for line := 1; line <= 5; line++ {
		diags = append(diags, smap.Diag{File: "/tmp/a.smap", Line: line, Level: "error", Msg: "same message"})
	}
	diags = append(diags, smap.Diag{File: "/tmp/a.smap", Line: 6, Level: "error", Msg: "other message"})
	got := diagnosticLines("/tmp/a.smap", true, diags)
	want := []string{
		"/tmp/a.smap:1: error: same message",
		"/tmp/a.smap:2: error: same message",
		"/tmp/a.smap:3: error: same message",
		"(and 2 more like this in /tmp/a.smap)",
		"/tmp/a.smap:6: error: other message",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("collapsed lines = %q, want %q", got, want)
	}

	var perFile []smap.Diag
	for _, file := range []string{"/tmp/a.smap", "/tmp/b.smap"} {
		for line := 1; line <= 4; line++ {
			perFile = append(perFile, smap.Diag{File: file, Line: line, Level: "error", Msg: "same"})
		}
	}
	got = diagnosticLines("/tmp/root.smap", true, perFile)
	if strings.Count(strings.Join(got, "\n"), "same") != 6 {
		t.Fatalf("each file keeps three lines, got %q", got)
	}
	for _, want := range []string{
		"(and 1 more like this in /tmp/a.smap)",
		"(and 1 more like this in /tmp/b.smap)",
	} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Fatalf("collapsed lines = %q, want %q", got, want)
		}
	}
}

func TestRunPrintsOnlyAWarningSummaryOutsideCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "root.smap")
	if err := os.WriteFile(path, []byte("core shared \"S\"\n  biz app \"A\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := captureRun(t, "outline", path)
	if code != 0 {
		t.Fatalf("outline exit = %d", code)
	}
	if !strings.Contains(out, `[biz] app "A"`) {
		t.Fatalf("outline stdout = %q", out)
	}
	if errOut != "note: 1 warning (run 'sysmap check' to see them)\n" {
		t.Fatalf("outline stderr = %q", errOut)
	}

	code, out, errOut = captureRun(t, "check", path)
	if code != 0 {
		t.Fatalf("check exit = %d", code)
	}
	if !strings.Contains(errOut, `business node "app" sits inside shared node "shared"`) || strings.Contains(errOut, "note:") {
		t.Fatalf("check stderr = %q", errOut)
	}
	if !strings.Contains(out, "ok: 2 nodes") {
		t.Fatalf("check stdout = %q", out)
	}
}

func TestRunStillPrintsErrorsAndCollapsesRepeats(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.smap")
	if err := os.WriteFile(bad, []byte("biz a \"A\"\nbiz b \"B\"\na -> ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := captureRun(t, "outline", bad)
	if code != 1 || out != "" {
		t.Fatalf("outline with an error = exit %d, stdout %q", code, out)
	}
	if !strings.Contains(errOut, `unknown node "ghost"`) || strings.Contains(errOut, "note:") {
		t.Fatalf("error stderr = %q", errOut)
	}

	repeated := filepath.Join(dir, "repeated.smap")
	if err := os.WriteFile(repeated, []byte("core root\n   core a\n   core b\n   core c\n   core d\n   core e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = captureRun(t, "check", repeated)
	if code != 1 {
		t.Fatalf("repeated check exit = %d", code)
	}
	if got := strings.Count(errOut, "indent must be a multiple of 2 spaces"); got != 3 {
		t.Fatalf("printed %d repeated messages, want 3:\n%s", got, errOut)
	}
	if !strings.Contains(errOut, "(and 2 more like this in "+repeated+")") {
		t.Fatalf("stderr lacks the collapse line:\n%s", errOut)
	}
}

func TestFindCaveatsFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caveats.smap")
	src := "biz a \"A\"\n  | unverified: no load test\nbiz b \"B\"\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := captureRun(t, "find", "--caveats", path)
	if code != 0 || errOut != "" {
		t.Fatalf("find --caveats = exit %d, stderr %q", code, errOut)
	}
	want := "a [biz] \"A\" " + path + ":1\n  unverified: no load test\n"
	if out != want {
		t.Fatalf("find --caveats = %q, want %q", out, want)
	}
	code, _, errOut = captureRun(t, "find", "--caveats", "word", path)
	if code != 1 || !strings.Contains(errOut, "takes no search word") {
		t.Fatalf("find --caveats with a word = exit %d, stderr %q", code, errOut)
	}
}
