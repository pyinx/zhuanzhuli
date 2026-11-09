package main

import (
	"path/filepath"
	"time"

	"focus.local/macos-capabilities/internal/macos"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
)

// diagnoseChoice measures actual workspace activation and owned panel visibility.
// Session setup is scripted; this is not recorded as a human Dock interaction.
func (h *host) diagnoseChoice() {
	results := []map[string]any{}
	for i := 0; i < 3; i++ {
		r := map[string]any{"iteration": i + 1, "eventSource": "NSWorkspace-real", "sessionSetup": "scripted", "windowEvidence": "AppKit-real"}
		mygo.RunOnMain(func() {
			h.win.Show()
			if err := h.dispatch("start"); err != nil {
				r["error"] = err.Error()
			} else if err := h.dispatch("entry-complete"); err != nil {
				r["error"] = err.Error()
			}
		})
		if r["error"] != nil {
			results = append(results, r)
			break
		}
		if err := h.activateFixture("B"); err != nil {
			r["error"] = err.Error()
			results = append(results, r)
			break
		}
		mygo.RunOnMain(func() {
			r["hostActiveBefore"] = macos.SelfState().Active
		})
		time.Sleep(500 * time.Millisecond)
		mygo.RunOnMain(func() {
			if h.choice != nil {
				r["before"] = macos.WindowState(h.choice.NativeHandle())
			} else {
				r["error"] = "choice not created"
			}
		})
		time.Sleep(500 * time.Millisecond)
		mygo.RunOnMain(func() {
			if h.choice != nil {
				r["after"] = macos.WindowState(h.choice.NativeHandle())
			}
			if i < 2 {
				if err := h.dispatch("end"); err != nil {
					r["error"] = err.Error()
				}
			}
		})
		results = append(results, r)
	}
	if err := journal.WriteJSON(filepath.Join(h.dir, "choice-diagnostic.json"), results); err != nil {
		h.event("diagnostic-write-error", err.Error())
	}
}
