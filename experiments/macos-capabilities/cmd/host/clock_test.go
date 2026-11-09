package main

import (
	"testing"
	"time"

	"focus.local/macos-capabilities/internal/session"
)

func TestHostClockDoesNotConsumeCommandBudget(t *testing.T) {
	h := &host{controller: session.New("run", time.Now)}
	for i := 0; i < 10001; i++ {
		if err := h.dispatch("tick"); err != nil {
			t.Fatalf("clock refresh %d: %v", i, err)
		}
	}
	if h.seq != 0 {
		t.Fatal("clock refresh allocated command IDs")
	}
	if _, err := h.controller.Handle(session.Command{RunID: "run", ID: "end", Revision: h.controller.Snapshot().Revision, Kind: "end"}); err != nil {
		t.Fatal("clock refresh prevented end:", err)
	}
}
