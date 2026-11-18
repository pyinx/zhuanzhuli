//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDebugOptInLevelFilterAndExport(t *testing.T) {
	dir := t.TempDir()
	r := &debugRecorder{dir: dir}
	attempt := newDebugAttempt()
	r.record(debugAccessDenied, attempt, "", 0)
	if records, _, err := r.snapshot(); err != nil || len(records) != 0 {
		t.Fatalf("default logging is off: %v, %v", records, err)
	}
	r.setEnabled(true)
	r.record(debugRestoreFailed, attempt, "com.example.Editor", 0)
	r.record(debugFullscreenFailed, attempt, "/private/work/document.txt", -25204)
	r.setEnabled(false)
	r.record(debugWorkStarted, attempt, "", 0)
	records, failed, err := r.snapshot()
	if err != nil || failed || len(records) != 3 {
		t.Fatalf("recording should stop immediately and retain three records: %v, %t, %v", records, failed, err)
	}
	errorText, n := formatDebugRecords(records, debugError)
	if n != 1 || !strings.Contains(errorText, "ERROR") || !strings.Contains(errorText, "AX -25204") ||
		!strings.Contains(errorText, attempt) || strings.Contains(errorText, "document.txt") ||
		strings.Contains(errorText, "WARN") {
		t.Fatalf("unexpected filtered log: %q", errorText)
	}
	warnText, n := formatDebugRecords(records, debugWarn)
	if n != 1 || !strings.Contains(warnText, "com.example.Editor") {
		t.Fatalf("warning lost target or selection: %q", warnText)
	}
	dst := filepath.Join(t.TempDir(), "export.log")
	if err := exportDebugLog(dir, dst, errorText); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(dst)
	if err != nil || string(out) != errorText {
		t.Fatalf("export must equal the current view: %q, %v", out, err)
	}
	if err := exportDebugLog(dir, filepath.Join(dir, "debug.jsonl"), "bad"); err == nil {
		t.Fatal("export must not replace app data")
	}
	alias := filepath.Join(filepath.Dir(dst), "data-alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if err := exportDebugLog(dir, filepath.Join(alias, "data.json"), "bad"); err == nil {
		t.Fatal("export followed a directory alias into app data")
	}
}

func TestDebugRotationBoundsAndConcurrentSnapshots(t *testing.T) {
	dir := t.TempDir()
	r := &debugRecorder{dir: dir, enabled: true}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			for range 1300 {
				r.record(debugFullscreenConfirmed, "", "com.example.Editor", 0)
			}
		})
	}
	wg.Go(func() {
		for range 15 {
			if _, failed, err := r.snapshot(); err != nil || failed {
				t.Errorf("concurrent snapshot: failed=%t err=%v", failed, err)
			}
		}
	})
	wg.Wait()
	for _, name := range []string{"debug.jsonl", "debug.jsonl.previous"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() > debugFileLimit || info.Mode().Perm() != 0600 {
			t.Fatalf("retained %s exceeds bounds, is missing, or is not private: %v, %v", name, info, err)
		}
	}
	r.record(debugAppQuit, "", "", 0)
	records, failed, err := r.snapshot()
	if err != nil || failed || len(records) < 2 || len(records) >= 3901 || records[len(records)-1].Code != debugAppQuit {
		t.Fatalf("rotation must drop old records and preserve the latest: count=%d, failed=%t, err=%v", len(records), failed, err)
	}
}

func TestDebugReportsUnsafeOrUnreadableLocalFile(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(dst, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dst, filepath.Join(dir, "debug.jsonl")); err != nil {
		t.Fatal(err)
	}
	r := &debugRecorder{dir: dir, enabled: true}
	r.record(debugAppStarted, "", "", 0)
	if _, failed, err := r.snapshot(); err == nil || !failed {
		t.Fatalf("invalid local file must surface the read and write failures: %t, %v", failed, err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "untouched" {
		t.Fatalf("outside target was touched: %q, %v", got, err)
	}
	if err := os.Remove(filepath.Join(dir, "debug.jsonl")); err != nil {
		t.Fatal(err)
	}
	r.record(debugAppQuit, "", "", 0)
	if records, failed, err := r.snapshot(); err != nil || !failed || len(records) != 1 {
		t.Fatalf("a later successful write must not hide lost entries: %v, %t, %v", records, failed, err)
	}
}
