package journal

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeBackupFailurePreservesOriginalAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.json")
	if err := WriteJSON(path, File{Schema: 1, Entries: []Entry{}}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	backup := fmt.Sprintf("%s.v1-%x.backup", path, sum[:6])
	if err = os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if j.Begin("one", processRef(t), false) == nil {
		t.Fatal("upgrade continued without a valid original backup")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) || j.Snapshot().Schema != 1 || j.Pending() != 0 {
		t.Fatal("failed upgrade changed original or in-memory ownership")
	}
	if err = os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err = j.Begin("one", processRef(t), false); err != nil {
		t.Fatal("backup repair did not allow retry", err)
	}
	if j.Snapshot().Schema != 2 || j.Pending() != 1 {
		t.Fatal("retry did not create exactly one operation")
	}
}

func TestAtomicWriteFailureDoesNotPublishOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.json")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	ref := Ref{PID: 1, Launched: 1, Token: "test"}
	if j.Begin("one", ref, false) == nil || j.Pending() != 0 {
		t.Fatal("failed atomic replacement published an operation")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = j.Begin("one", ref, false); err != nil {
		t.Fatal("retry after atomic write failure", err)
	}
}

func processRef(t *testing.T) Ref {
	t.Helper()
	var r Ref
	if err := json.Unmarshal([]byte(`{"label":"owned","pid":123,"launched":123.25,"bundleID":"test.owned","path":"/test/Owned.app","token":"test","identitySource":"bsd-start-v1"}`), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestProcessIdentityUpgradesJournalAndRetainsVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.json")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := processRef(t)
	if err = j.Begin("one", r, false); err != nil {
		t.Fatal(err)
	}
	if err = j.Applied("one"); err != nil {
		t.Fatal(err)
	}
	if j.Snapshot().Schema != 2 || !j.CanRestore(r, true) {
		t.Fatal("new identity lost schema or restoration ownership", j.Snapshot())
	}
	reopened, err := Open(path)
	if err != nil || !reopened.CanRestore(r, true) {
		t.Fatal("new identity did not survive restart", err)
	}
	if err = reopened.Release(r); err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Schema != 2 {
		t.Fatal("a normal mutation downgraded the journal")
	}
	backups, _ := filepath.Glob(path + ".v1-*.backup")
	if len(backups) != 1 {
		t.Fatal("upgrade has no original-format backup", backups)
	}
	b, err := Open(backups[0])
	if err != nil || b.Snapshot().Schema != 1 || b.Pending() != 0 {
		t.Fatal("backup is not readable original state", err)
	}
}

func TestLegacyPendingPreventsIdentityUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.json")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := Ref{PID: 321, Launched: 12, Token: "old", BundleID: "test.old", Path: "/test/Old.app"}
	if err = j.Begin("old", r, false); err != nil {
		t.Fatal(err)
	}
	if err = j.Applied("old"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if j.Begin("new", processRef(t), false) == nil {
		t.Fatal("upgraded while legacy restoration was unresolved")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || !j.CanRestore(r, true) {
		t.Fatal("failed upgrade changed original ownership")
	}
	if err = j.Release(r); err != nil {
		t.Fatal(err)
	}
	if err = j.Begin("new", processRef(t), false); err != nil {
		t.Fatal("resolved legacy state still blocks upgrade", err)
	}
}

func TestUnknownIdentityAndSchemaAreRejectedWithoutWrites(t *testing.T) {
	for _, body := range []string{
		`{"schemaVersion":2,"entries":[{"operationID":"x","status":"applied","ref":{"pid":1,"launched":1,"token":"x","identitySource":"unknown"}}]}`,
		`{"schemaVersion":1,"entries":[{"operationID":"x","status":"applied","ref":{"pid":1,"launched":1,"token":"x","identitySource":"bsd-start-v1"}}]}`,
		`{"schemaVersion":3,"entries":[]}`,
	} {
		path := filepath.Join(t.TempDir(), "environment.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil {
			t.Fatal("accepted unknown identity/schema", body)
		}
		after, _ := os.ReadFile(path)
		if string(after) != body {
			t.Fatal("rejected data was overwritten")
		}
	}
}

func TestDowngradeRequiresResolvedStateAndKeepsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.json")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := processRef(t)
	if err = j.Begin("one", r, false); err != nil {
		t.Fatal(err)
	}
	d, ok := any(j).(interface{ Downgrade() error })
	if !ok {
		t.Fatal("safe downgrade is unavailable")
	}
	before, _ := os.ReadFile(path)
	if d.Downgrade() == nil {
		t.Fatal("downgraded pending ownership")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("refused downgrade changed data")
	}
	if err = j.Applied("one"); err != nil {
		t.Fatal(err)
	}
	if err = j.Release(r); err != nil {
		t.Fatal(err)
	}
	if err = d.Downgrade(); err != nil {
		t.Fatal(err)
	}
	if j.Snapshot().Schema != 1 || j.Pending() != 0 {
		t.Fatal("downgrade did not produce closed legacy state")
	}
	if _, err = Open(path); err != nil {
		t.Fatal("downgraded file is unreadable", err)
	}
	backups, _ := filepath.Glob(path + ".v2-*.backup")
	if len(backups) != 1 {
		t.Fatal("missing v2 rollback backup", backups)
	}
}

// Compile the exact pre-change reader, not a mock of its version check.
func TestLegacyReaderRejectsNewJournalWithoutChangingIt(t *testing.T) {
	dir := t.TempDir()
	source, err := os.ReadFile("testdata/legacy_v1.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	source = []byte(strings.Replace(string(source), "package journal", "package main", 1))
	if err = os.WriteFile(filepath.Join(dir, "legacy.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	main := `package main
import "os"
func main(){_,err:=Open(os.Args[1]);if err==nil {os.Exit(2)};println(err.Error())}
`
	if err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "environment.json")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Begin("one", processRef(t), false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	out, err := exec.Command("go", "run", filepath.Join(dir, "legacy.go"), filepath.Join(dir, "main.go"), path).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "unsupported: journal schema") {
		t.Fatalf("old reader did not reject v2: %v %s", err, out)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("legacy reader changed v2 state")
	}
}
