//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

var buildVersion = "development"
var buildID = "unbundled"

// Bounded, local and deliberately content-free: no goal, document path or raw
// OS error text. Failure to log never changes recovery ownership.
func (a *application) diagnostic(stage, code string) {
	if a.dir == "" || a.journal == nil {
		return
	}
	path := filepath.Join(a.dir, "diagnostics.jsonl")
	if info, err := os.Stat(path); err == nil && info.Size() > 64*1024 {
		if err = os.Rename(path, path+".previous"); err != nil {
			return
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(map[string]any{"at": time.Now().UTC(), "build": buildID, "version": buildVersion, "stage": stage, "code": code, "pending": a.journal.Pending()})
}
