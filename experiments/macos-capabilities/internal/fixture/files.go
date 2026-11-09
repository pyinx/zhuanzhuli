// Package fixture validates the local test run boundary shared by the three bundles.
package fixture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"focus.local/zhuanzhuli/pkg/journal"
)

var runName = regexp.MustCompile(`^run-[0-9]{8}T[0-9]{6}-[a-f0-9]{8}$`)

type State struct {
	Schema  int         `json:"schemaVersion"`
	Ref     journal.Ref `json:"ref"`
	Counter uint64      `json:"counter"`
	Hidden  bool        `json:"hidden"`
	Active  bool        `json:"active"`
	Windows []int       `json:"windows"`
}

func RunDir(root, run string) (string, error) {
	if !runName.MatchString(run) {
		return "", errors.New("invalid: run ID")
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(r, run)
	actual, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if actual != p {
		return "", errors.New("invalid: symlink run")
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", errors.New("invalid: run directory")
	}
	return p, nil
}
func ReadState(path string, token string) (State, error) {
	var s State
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return s, errors.New("invalid: state file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if len(b) > 65536 {
		return s, errors.New("invalid: oversized state")
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Schema != 1 || s.Ref.Token != token || s.Ref.PID < 1 || s.Ref.Launched <= 0 {
		return s, errors.New("identity-changed: invalid handshake")
	}
	return s, nil
}

func StopRequested(path, token string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 || len(token) != 32 {
		return false
	}
	b, err := os.ReadFile(path)
	var request struct {
		Token string `json:"token"`
		Stop  bool   `json:"stop"`
	}
	return err == nil && json.Unmarshal(b, &request) == nil && request.Token == token && request.Stop
}
