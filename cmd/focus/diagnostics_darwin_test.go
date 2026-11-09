//go:build darwin

package main

import (
	"focus.local/zhuanzhuli/pkg/journal"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsRotateAndDoNotSerializeWorkContent(t *testing.T) {
	dir := t.TempDir()
	j, err := journal.Open(filepath.Join(dir, "environment.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := &application{dir: dir, journal: j, draftGoal: "PRIVATE WORK DOCUMENT"}
	for i := 0; i < 1500; i++ {
		a.diagnostic("preparation", "failed")
	}
	for _, name := range []string{"diagnostics.jsonl", "diagnostics.jsonl.previous"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 66*1024 || strings.Contains(string(data), a.draftGoal) || !strings.Contains(string(data), `"stage":"preparation"`) {
			t.Fatal("diagnostics are unbounded, include work content, or omit operation stage")
		}
	}
}
