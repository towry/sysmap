package main

import (
	"os"
	"path/filepath"
	"testing"
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
