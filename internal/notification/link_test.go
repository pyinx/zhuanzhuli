package notification

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// In-memory drivers exercise lease policy only, not macOS or notification delivery.
type testPanel struct {
	modes    map[string]bool
	sets     int
	setErr   error
	readErr  error
	noEffect bool
}

func (p *testPanel) Modes() (map[string]bool, error) {
	copy := map[string]bool{}
	for k, v := range p.modes {
		copy[k] = v
	}
	return copy, p.readErr
}
func (p *testPanel) Set(n string, on bool) error {
	p.sets++
	if !p.noEffect {
		p.modes[n] = on
	}
	return p.setErr
}

type testDriver struct {
	p            *testPanel
	err          error
	cleanupErr   error
	opens        int
	onOpen       func(int)
	cleanupErrAt int
}

func (d *testDriver) WithPanel(f func(Panel) error) error {
	d.opens++
	if d.onOpen != nil {
		d.onOpen(d.opens)
	}
	if d.err != nil {
		return d.err
	}
	err := f(d.p)
	if d.cleanupErrAt == 0 || d.cleanupErrAt == d.opens {
		err = errors.Join(err, d.cleanupErr)
	}
	return err
}

func TestPanelCleanupFailureCannotEnableFocus(t *testing.T) {
	l, d := newLink(t)
	d.cleanupErr = errors.New("panel did not close")
	if l.Acquire(d, "专注力", yes) == nil || d.p.sets != 0 || l.Pending() || d.p.modes["专注力"] {
		t.Fatal("failed read-only panel round trip still changed system Focus")
	}
}
func newLink(t *testing.T) (*Link, *testDriver) {
	t.Helper()
	l, err := Open(filepath.Join(t.TempDir(), "lease.json"))
	if err != nil {
		t.Fatal(err)
	}
	return l, &testDriver{p: &testPanel{modes: map[string]bool{"专注力": false, "Work": false}}}
}
func yes() bool { return true }
func TestSameActiveModeRequiresReviewAndRepeatedCleanupDoesNothing(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	if !d.p.modes["专注力"] || !l.Pending() {
		t.Fatal("activation was not observed and recorded")
	}
	if err := l.Restore(d); !errors.Is(err, ErrUncertain) {
		t.Fatalf("same mode value cannot prove ownership: %v", err)
	}
	if !d.p.modes["专注力"] || !l.Pending() || d.p.sets != 1 {
		t.Fatal("uncertain ownership changed the mode or lost the pending record")
	}
	if err := l.KeepCurrent(); err != nil {
		t.Fatal(err)
	}
	opens := d.opens
	if err := l.Restore(d); err != nil || d.opens != opens {
		t.Fatal("repeat restoration had effects")
	}
}
func TestPreexistingMatchingModeIsNeverTurnedOff(t *testing.T) {
	l, d := newLink(t)
	d.p.modes["专注力"] = true
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	if err := l.Restore(d); err != nil || !d.p.modes["专注力"] || d.p.sets != 0 || l.Pending() {
		t.Fatal("borrowed mode was taken over")
	}
}
func TestForeignModeAndMissingProfileArePreserved(t *testing.T) {
	for _, name := range []string{"专注力", "missing"} {
		l, d := newLink(t)
		d.p.modes["Work"] = true
		if l.Acquire(d, name, yes) == nil || d.p.sets != 0 || l.Pending() {
			t.Fatal("foreign mode or missing profile accepted")
		}
	}
}
func TestUnacknowledgedMutationRemainsUncertain(t *testing.T) {
	l, d := newLink(t)
	d.p.setErr = errors.New("lost acknowledgement")
	if l.Acquire(d, "专注力", yes) == nil || !l.Pending() {
		t.Fatal("unknown mutation lost")
	}
	if !errors.Is(l.Restore(d), ErrUncertain) || d.p.sets != 1 {
		t.Fatal("unknown mutation silently restored")
	}
	if err := l.KeepCurrent(); err != nil || l.Pending() || !d.p.modes["专注力"] {
		t.Fatal("explicit review changed system")
	}
}
func TestReopenedLeaseCannotClaimContinuousOwnership(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(l.path)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(reopened.Restore(d), ErrUncertain) || d.p.sets != 1 || !d.p.modes["专注力"] {
		t.Fatal("restart claimed ownership")
	}
}
func TestExternalModeChangeSurvivesCleanup(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	d.p.modes["专注力"], d.p.modes["Work"] = false, true
	if err := l.Restore(d); err != nil || !d.p.modes["Work"] || d.p.sets != 1 || l.Pending() {
		t.Fatal("external change overwritten")
	}
}

func TestExternalSameModeReactivationCannotBeUndone(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	// The user switched the mode off and back on outside this process. The
	// public panel exposes the same value as before, not a change generation.
	d.p.modes["专注力"] = false
	d.p.modes["专注力"] = true
	if !errors.Is(l.Restore(d), ErrUncertain) || d.p.sets != 1 || !d.p.modes["专注力"] || !l.Pending() {
		t.Fatal("same mode value was mistaken for continuous ownership; external activation was undone")
	}
}

func TestAlreadyDisabledModeReleasesWithoutAnotherSwitch(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	d.p.modes["专注力"] = false
	if err := l.Restore(d); err != nil || l.Pending() || d.p.sets != 1 {
		t.Fatalf("an externally disabled mode must release without another switch: %v", err)
	}
}

func TestReviewWriteFailureRetainsUncertainLease(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	path := l.path
	l.path = filepath.Join(path, "missing", "lease.json")
	if l.KeepCurrent() == nil || !l.Pending() {
		t.Fatal("failed review save lost the pending lease")
	}
	l.path = path
	if !errors.Is(l.Restore(d), ErrUncertain) || d.p.sets != 1 || !d.p.modes["专注力"] {
		t.Fatal("failed review save allowed automatic shutdown")
	}
	if err := l.KeepCurrent(); err != nil || l.Pending() || !d.p.modes["专注力"] {
		t.Fatalf("explicit review retry did not commit without system changes: %v", err)
	}
}
func TestCancellationBeforeMutationHasNoEffect(t *testing.T) {
	for _, cancelAt := range []int{1, 2} {
		l, d := newLink(t)
		calls := 0
		_ = l.Acquire(d, "专注力", func() bool { calls++; return calls < cancelAt })
		if d.p.sets != 0 || l.Pending() {
			t.Fatal("late mutation after pause/end")
		}
	}
}
func TestFailedJournalWriteCannotSendSystemChange(t *testing.T) {
	l, d := newLink(t)
	l.path = filepath.Join(l.path, "missing", "lease.json")
	if l.Acquire(d, "专注力", yes) == nil || d.p.sets != 0 || l.Pending() {
		t.Fatal("mutation without durable intent")
	}
}
func TestPermissionOrReadFailureDoesNotCreateLease(t *testing.T) {
	for _, read := range []bool{false, true} {
		l, d := newLink(t)
		if read {
			d.p.readErr = errors.New("unknown state")
		} else {
			d.err = errors.New("not trusted")
		}
		if l.Acquire(d, "专注力", yes) == nil || l.Pending() || d.p.sets != 0 {
			t.Fatal("failure became protection")
		}
	}
}
func TestNoEffectIsNotRecordedAsApplied(t *testing.T) {
	l, d := newLink(t)
	d.p.noEffect = true
	if l.Acquire(d, "专注力", yes) == nil || !l.Pending() || l.live {
		t.Fatal("request counted as actual effect")
	}
}
func TestCorruptLeasePreserved(t *testing.T) {
	for _, data := range []string{`{`, `{"version":2,"status":"released"}`, `{"version":1,"status":"applied","profile":""}`} {
		path := filepath.Join(t.TempDir(), "lease.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil {
			t.Fatal("corrupt lease accepted")
		}
		got, _ := os.ReadFile(path)
		if string(got) != data {
			t.Fatal("corrupt lease overwritten")
		}
	}
}

func TestExplicitCloseCanResolveReopenedLeaseAndIsIdempotent(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	l, err := Open(l.path)
	if err != nil {
		t.Fatal(err)
	}
	if l.PendingProfile() != "专注力" {
		t.Fatal("pending profile missing")
	}
	if err := l.CloseConfirmed(d, "专注力"); err != nil {
		t.Fatal(err)
	}
	if l.Pending() || l.PendingProfile() != "" || d.p.modes["专注力"] || d.p.sets != 2 {
		t.Fatal("explicit close was not observed and committed")
	}
	opens := d.opens
	d.p.modes["专注力"] = true // A later external activation must not be affected by repeated cleanup.
	if err := l.CloseConfirmed(d, "专注力"); err != nil || d.opens != opens || !d.p.modes["专注力"] {
		t.Fatal("repeat close affected a later mode")
	}
}

func TestExplicitCloseRejectsStaleOrForeignMode(t *testing.T) {
	for _, changed := range []string{"profile", "foreign", "missing"} {
		t.Run(changed, func(t *testing.T) {
			l, d := newLink(t)
			if err := l.Acquire(d, "专注力", yes); err != nil {
				t.Fatal(err)
			}
			profile := "专注力"
			switch changed {
			case "profile":
				profile = "Work"
			case "foreign":
				d.p.modes["专注力"], d.p.modes["Work"] = false, true
			case "missing":
				delete(d.p.modes, "专注力")
			}
			if l.CloseConfirmed(d, profile) == nil || !l.Pending() || d.p.sets != 1 {
				t.Fatal("stale review switched a mode or lost the record")
			}
		})
	}
}

func TestExplicitCloseAlreadyOffReleasesWithoutSwitch(t *testing.T) {
	l, d := newLink(t)
	if err := l.Acquire(d, "专注力", yes); err != nil {
		t.Fatal(err)
	}
	d.p.modes["专注力"] = false
	if err := l.CloseConfirmed(d, "专注力"); err != nil || l.Pending() || d.p.sets != 1 {
		t.Fatal("already-off close toggled the mode")
	}
}

func TestExplicitCloseFailuresRemainPendingAndRetryable(t *testing.T) {
	for _, failure := range []string{"permission", "read", "no-effect", "acknowledgement", "write"} {
		t.Run(failure, func(t *testing.T) {
			l, d := newLink(t)
			if err := l.Acquire(d, "专注力", yes); err != nil {
				t.Fatal(err)
			}
			original := l.path
			switch failure {
			case "permission":
				d.err = errors.New("permission denied")
			case "read":
				d.p.readErr = errors.New("unknown state")
			case "no-effect":
				d.p.noEffect = true
			case "acknowledgement":
				d.p.setErr = errors.New("lost acknowledgement")
			case "write":
				l.path = filepath.Join(original, "unwritable", "lease.json")
			}
			if l.CloseConfirmed(d, "专注力") == nil || !l.Pending() {
				t.Fatal("failed close lost pending state")
			}
			d.err, d.p.readErr, d.p.setErr, d.p.noEffect, l.path = nil, nil, nil, false, original
			if err := l.CloseConfirmed(d, "专注力"); err != nil || l.Pending() || d.p.modes["专注力"] {
				t.Fatalf("close retry failed: %v", err)
			}
		})
	}
}

func TestExplicitCloseRechecksAfterPreflightAndPreservesCleanupFailure(t *testing.T) {
	for _, changed := range []bool{true, false} {
		l, d := newLink(t)
		if err := l.Acquire(d, "专注力", yes); err != nil {
			t.Fatal(err)
		}
		if changed {
			d.onOpen = func(n int) {
				if n == 4 {
					d.p.modes["专注力"], d.p.modes["Work"] = false, true
				}
			}
		} else {
			d.cleanupErr, d.cleanupErrAt = errors.New("panel did not close"), 4
		}
		if l.CloseConfirmed(d, "专注力") == nil || !l.Pending() {
			t.Fatal("failed final read/cleanup lost pending review")
		}
		if changed && (d.p.sets != 1 || !d.p.modes["Work"]) {
			t.Fatal("preflight was mistaken for a mutation authorization")
		}
		if !changed {
			if d.p.modes["专注力"] {
				t.Fatal("close was not sent")
			}
			d.cleanupErr = nil
			if err := l.CloseConfirmed(d, "专注力"); err != nil || l.Pending() || d.p.sets != 2 {
				t.Fatal("cleanup retry toggled the mode again")
			}
		}
	}
}
