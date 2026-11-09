package fixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"focus.local/zhuanzhuli/pkg/journal"
)

func TestStopRequiresCurrentTokenRegularBoundedFile(t *testing.T) {
	token := strings.Repeat("a", 32)
	p := filepath.Join(t.TempDir(), "host-stop.json")
	for _, request := range []map[string]any{{"token": "wrong", "stop": true}, {"token": token, "stop": false}, {"token": token, "stop": true}} {
		if err := journal.WriteJSON(p, request); err != nil {
			t.Fatal(err)
		}
		want := request["token"] == token && request["stop"] == true
		if StopRequested(p, token) != want {
			t.Fatal("incorrect stop authorization")
		}
	}
	if err := os.WriteFile(p, []byte(strings.Repeat(" ", 1025)), 0600); err != nil {
		t.Fatal(err)
	}
	if StopRequested(p, token) {
		t.Fatal("accepted oversized stop")
	}
	link := p + "-link"
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if StopRequested(link, token) {
		t.Fatal("accepted stop symlink")
	}
}

func TestRunCannotEscapeOrUseSymlink(t *testing.T) {
	d := t.TempDir()
	run := "run-20261022T120000-aabbccdd"
	if err := os.Mkdir(filepath.Join(d, run), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := RunDir(d, run); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"../other", "latest", "", run + "/../x"} {
		if _, err := RunDir(d, s); err == nil {
			t.Fatal("accepted unsafe path")
		}
	}
	if err := os.Remove(filepath.Join(d, run)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(d, run)); err != nil {
		t.Fatal(err)
	}
	if _, err := RunDir(d, run); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestHandshakeMustMatchTokenAndSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "B.json")
	s := State{Schema: 1, Ref: journal.Ref{Token: "one", PID: 3, Launched: 1}}
	if err := journal.WriteJSON(p, s); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(p, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(p, "two"); err == nil {
		t.Fatal("accepted other run")
	}
	s.Schema = 2
	if err := journal.WriteJSON(p, s); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(p, "one"); err == nil {
		t.Fatal("accepted future schema")
	}
}
