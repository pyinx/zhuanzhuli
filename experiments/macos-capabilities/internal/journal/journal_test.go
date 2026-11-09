package journal_test

import (
	shared "focus.local/zhuanzhuli/pkg/journal"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreOnlyOwnedInstanceAndObservedChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	j, err := shared.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := shared.Ref{Label: "B", PID: 123, Launched: 123.5, BundleID: "local.focusprobe.fixture.b", Path: "/test/B.app", Token: "token"}
	if err = j.Begin("one", r, false); err != nil {
		t.Fatal(err)
	}
	if j.CanRestore(r, true) {
		t.Fatal("pending request claimed ownership")
	}
	if !j.HasUnknown(r) {
		t.Fatal("forgot pending request; visible now cannot prove cleanup")
	}
	if err = j.Applied("one"); err != nil {
		t.Fatal(err)
	}
	j, err = shared.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !j.CanRestore(r, true) {
		t.Fatal("lost applied operation after restart")
	}
	if j.HasUnknown(r) {
		t.Fatal("applied state still unknown")
	}
	other := r
	other.PID++
	if j.CanRestore(other, true) || j.CanRestore(r, false) {
		t.Fatal("restores wrong instance or user change")
	}
	if err = j.Abandon(r, "external event"); err != nil {
		t.Fatal(err)
	}
	if j.CanRestore(r, true) {
		t.Fatal("reclaimed abandoned ownership")
	}
}

func TestInitiallyHiddenAndRepeatedCleanup(t *testing.T) {
	j, err := shared.Open(filepath.Join(t.TempDir(), "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := shared.Ref{Label: "B", PID: 1, Launched: 1, BundleID: "id", Path: "/x", Token: "t"}
	if err = j.Begin("hidden", r, true); err != nil {
		t.Fatal(err)
	}
	if j.CanRestore(r, true) {
		t.Fatal("owns original hidden state")
	}
	if err = j.Release(r); err != nil {
		t.Fatal(err)
	}
	if err = j.Release(r); err != nil {
		t.Fatal(err)
	}
	if j.Pending() != 0 {
		t.Fatal("cleanup not idempotent")
	}
}

func TestCorruptOrUnknownJournalIsNeverOverwritten(t *testing.T) {
	for _, data := range []string{`broken`, `{"schemaVersion":99,"entries":[]}`} {
		p := filepath.Join(t.TempDir(), "journal.json")
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := shared.Open(p); err == nil {
			t.Fatal("accepted corrupt schema")
		}
		got, err := os.ReadFile(p)
		if err != nil || string(got) != data {
			t.Fatal("overwrote evidence")
		}
	}
}

func TestPersistenceFailureDoesNotPublishOwnership(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "journal.json")
	j, err := shared.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	r := shared.Ref{Label: "B", PID: 1, Launched: 1, BundleID: "id", Path: "/x", Token: "t"}
	if err = j.Begin("operation", r, false); err == nil {
		t.Fatal("ignored persistence failure")
	}
	if j.Pending() != 0 {
		t.Fatal("published unpersisted operation")
	}
}
