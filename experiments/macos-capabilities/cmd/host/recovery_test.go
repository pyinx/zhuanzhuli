package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"focus.local/zhuanzhuli/pkg/journal"
)

func TestUnknownPendingCannotBeReleasedAsAlreadyVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	ledger, err := journal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := journal.Ref{Label: "B", PID: 42, Launched: 1, Token: "synthetic"}
	if err := ledger.Begin("between-call-and-observation", ref, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &host{ledger: ledger, refs: map[string]journal.Ref{"B": ref}}
	if err := h.cleanup(); err == nil || !strings.Contains(err.Error(), "unknown outcome") {
		t.Fatal("must reject before inspecting or changing any application:", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || ledger.Pending() != 1 {
		t.Fatal("unknown recovery evidence was changed")
	}
}
