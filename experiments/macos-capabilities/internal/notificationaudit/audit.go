// Package notificationaudit runs the same read-only query inside each owned bundle.
package notificationaudit

import (
	"errors"
	"path/filepath"
	"sync"
	"time"

	"focus.local/macos-capabilities/internal/macos"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
)

func Run(dir, token, label string) error {
	var once sync.Once
	finished := make(chan struct{})
	finish := func(facts map[string]any, err error) {
		once.Do(func() {
			close(finished)
			mygo.RunOnMain(func() {
				report := map[string]any{"schemaVersion": 1, "evidenceKind": "automated-real", "at": time.Now().UTC(), "facts": facts}
				if self, selfErr := macos.Self(label, token); selfErr == nil {
					report["ref"] = self
				} else {
					report["error"] = selfErr.Error()
				}
				if err != nil {
					report["error"] = err.Error()
				}
				if writeErr := journal.WriteJSON(filepath.Join(dir, label+"-notification-audit.json"), report); writeErr != nil {
					panic(writeErr)
				}
				mygo.App.Quit()
			})
		})
	}
	mygo.App.WhenReady(func() {
		macos.NotificationFacts(finish)
		go func() {
			select {
			case <-finished:
			case <-time.After(15 * time.Second):
				finish(nil, errors.New("read-only notification query timed out"))
			}
		}()
	})
	return mygo.App.Run()
}
