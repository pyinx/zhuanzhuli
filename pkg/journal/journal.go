package journal

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
)

type Ref struct {
	Label          string  `json:"label"`
	PID            int     `json:"pid"`
	Launched       float64 `json:"launched"`
	BundleID       string  `json:"bundleID"`
	Path           string  `json:"path"`
	Token          string  `json:"token"`
	IdentitySource string  `json:"identitySource,omitempty"`
}

const IdentityBSDStart = "bsd-start-v1"

var ErrLegacyPending = errors.New("旧环境改动尚未恢复，不能升级实例身份")

func validRef(r Ref) bool {
	if r.PID < 1 || r.Launched <= 0 || math.IsNaN(r.Launched) || math.IsInf(r.Launched, 0) || r.Token == "" {
		return false
	}
	switch r.IdentitySource {
	case "":
		return true
	case IdentityBSDStart:
		return r.BundleID != "" && filepath.IsAbs(r.Path) && filepath.Clean(r.Path) != "/"
	default:
		return false
	}
}

type Entry struct {
	ID     string `json:"operationID"`
	Ref    Ref    `json:"ref"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}
type File struct {
	Schema  int     `json:"schemaVersion"`
	Entries []Entry `json:"entries"`
}
type Journal struct {
	mu   sync.Mutex
	path string
	data File
}

func Open(path string) (*Journal, error) {
	j := &Journal{path: path, data: File{Schema: 1, Entries: []Entry{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &j.data); err != nil {
		return nil, err
	}
	if j.data.Schema != 1 && j.data.Schema != 2 {
		return nil, errors.New("unsupported: journal schema")
	}
	for _, e := range j.data.Entries {
		if e.ID == "" || !validRef(e.Ref) || (j.data.Schema == 1 && e.Ref.IdentitySource != "") {
			return nil, errors.New("invalid: journal entry")
		}
		switch e.Status {
		case "pending", "applied", "released", "abandoned":
		default:
			return nil, errors.New("invalid: journal status")
		}
	}
	return j, nil
}
func WriteJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(b, '\n'))
}

// WriteFile atomically replaces a local file with user-only permissions.
func WriteFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".atomic-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Preserve the exact old bytes before changing the reader contract. A digest
// makes retries idempotent; an existing different backup is never overwritten.
func (j *Journal) backup() error {
	b, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		b, err = json.MarshalIndent(j.data, "", "  ")
	}
	if err != nil {
		return err
	}
	digest := sha256.Sum256(b)
	path := fmt.Sprintf("%s.v%d-%x.backup", j.path, j.data.Schema, digest[:6])
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("恢复日志备份位置不可用")
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(old, b) {
			return errors.New("恢复日志备份不一致，原记录已保留")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return WriteFile(path, b)
}

func (j *Journal) commit(next File) error {
	if next.Schema != j.data.Schema {
		if err := j.backup(); err != nil {
			return err
		}
	}
	if err := WriteJSON(j.path, next); err != nil {
		return err
	}
	j.data = next
	return nil
}
func (j *Journal) mutate(fn func(*File) error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	next := File{Schema: j.data.Schema, Entries: append([]Entry{}, j.data.Entries...)}
	if err := fn(&next); err != nil {
		return err
	}
	return j.commit(next)
}
func (j *Journal) Begin(id string, r Ref, initialHidden bool) error {
	if initialHidden {
		return nil
	}
	if id == "" || !validRef(r) {
		return errors.New("invalid: operation identity")
	}
	return j.mutate(func(f *File) error {
		if r.IdentitySource != "" && f.Schema == 1 {
			for _, e := range f.Entries {
				if e.Status == "pending" || e.Status == "applied" {
					return ErrLegacyPending
				}
			}
			f.Schema = 2
		}
		for _, e := range f.Entries {
			if e.ID == id {
				return errors.New("conflict: operation already exists")
			}
		}
		f.Entries = append(f.Entries, Entry{ID: id, Ref: r, Status: "pending"})
		return nil
	})
}
func (j *Journal) Applied(id string) error {
	return j.mutate(func(f *File) error {
		for i := range f.Entries {
			if f.Entries[i].ID == id && f.Entries[i].Status == "pending" {
				f.Entries[i].Status = "applied"
				return nil
			}
		}
		return errors.New("conflict: no pending operation")
	})
}
func (j *Journal) CanRestore(r Ref, hidden bool) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !hidden {
		return false
	}
	for _, e := range j.data.Entries {
		if e.Ref == r && e.Status == "applied" {
			return true
		}
	}
	return false
}
func (j *Journal) HasUnknown(r Ref) bool {
	for _, e := range j.Snapshot().Entries {
		if e.Ref == r && e.Status == "pending" {
			return true
		}
	}
	return false
}
func (j *Journal) Abandon(r Ref, reason string) error {
	return j.mutate(func(f *File) error {
		for i := range f.Entries {
			e := &f.Entries[i]
			if e.Ref == r && (e.Status == "pending" || e.Status == "applied") {
				e.Status = "abandoned"
				e.Reason = reason
			}
		}
		return nil
	})
}
func (j *Journal) Release(r Ref) error {
	return j.mutate(func(f *File) error {
		for i := range f.Entries {
			if f.Entries[i].Ref == r && f.Entries[i].Status == "applied" {
				f.Entries[i].Status = "released"
			}
		}
		return nil
	})
}

// AbandonPending closes an entire batch without applying any recorded effects.
func (j *Journal) AbandonPending(reason string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	next := File{Schema: j.data.Schema, Entries: append([]Entry{}, j.data.Entries...)}
	changed := false
	for i := range next.Entries {
		e := &next.Entries[i]
		if e.Status == "pending" || e.Status == "applied" {
			e.Status, e.Reason = "abandoned", reason
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return j.commit(next)
}
func (j *Journal) Snapshot() File {
	j.mu.Lock()
	defer j.mu.Unlock()
	return File{Schema: j.data.Schema, Entries: append([]Entry{}, j.data.Entries...)}
}

// Downgrade is explicit and refuses unresolved ownership. Closed entries have
// no operation rights, so their source metadata can be removed for the v1 reader.
func (j *Journal) Downgrade() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, e := range j.data.Entries {
		if e.Status == "pending" || e.Status == "applied" {
			return errors.New("仍有待恢复改动，不能回退恢复日志")
		}
	}
	if j.data.Schema == 1 {
		return nil
	}
	next := File{Schema: 1, Entries: append([]Entry{}, j.data.Entries...)}
	for i := range next.Entries {
		next.Entries[i].Ref.IdentitySource = ""
	}
	return j.commit(next)
}
func (j *Journal) Pending() int {
	n := 0
	for _, e := range j.Snapshot().Entries {
		if e.Status == "pending" || e.Status == "applied" {
			n++
		}
	}
	return n
}
