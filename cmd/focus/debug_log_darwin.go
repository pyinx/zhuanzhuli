//go:build darwin

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"focus.local/zhuanzhuli/pkg/journal"
)

type debugLevel string
type debugCode string

const (
	debugInfo  debugLevel = "INFO"
	debugWarn  debugLevel = "WARN"
	debugError debugLevel = "ERROR"

	debugAppStarted          debugCode = "app-started"
	debugAppQuit             debugCode = "app-quit"
	debugEnabled             debugCode = "debug-enabled"
	debugStart               debugCode = "session-start"
	debugImmersionStart      debugCode = "immersive-start"
	debugResume              debugCode = "session-resume"
	debugPaused              debugCode = "session-paused"
	debugEnded               debugCode = "session-ended"
	debugWorkStarted         debugCode = "work-started"
	debugStartFailed         debugCode = "start-failed"
	debugPrepareFailed       debugCode = "environment-prepare-failed"
	debugStorageFailed       debugCode = "environment-storage-failed"
	debugAccessDenied        debugCode = "accessibility-denied"
	debugAccessFailed        debugCode = "accessibility-check-failed"
	debugAccessGranted       debugCode = "accessibility-granted"
	debugTargetFailed        debugCode = "work-target-unavailable"
	debugWindowReady         debugCode = "work-window-ready"
	debugWindowUnsupported   debugCode = "work-window-unsupported"
	debugFullscreenPreserved debugCode = "fullscreen-preserved"
	debugFullscreenRequest   debugCode = "fullscreen-request"
	debugFullscreenFailed    debugCode = "fullscreen-failed"
	debugFullscreenConfirmed debugCode = "fullscreen-confirmed"
	debugHandoffFailed       debugCode = "handoff-failed"
	debugHandoffConfirmed    debugCode = "handoff-confirmed"
	debugWindowedHandoff     debugCode = "handoff-without-fullscreen"
	debugRestoreFailed       debugCode = "restore-unconfirmed"
	debugRestoreConfirmed    debugCode = "fullscreen-restored"
)

type debugDescription struct {
	level       debugLevel
	stage, text string
}

var debugDescriptions = map[debugCode]debugDescription{
	debugAppStarted:          {debugInfo, "app", "归径已启动"},
	debugAppQuit:             {debugInfo, "app", "归径正常退出"},
	debugEnabled:             {debugInfo, "app", "已开启调试日志"},
	debugStart:               {debugInfo, "session", "开始准备普通专注"},
	debugImmersionStart:      {debugInfo, "session", "开始准备沉浸专注"},
	debugResume:              {debugInfo, "session", "尝试恢复专注"},
	debugPaused:              {debugInfo, "session", "专注已暂停"},
	debugEnded:               {debugInfo, "session", "专注已结束"},
	debugWorkStarted:         {debugInfo, "session", "工作计时已开始"},
	debugStartFailed:         {debugError, "session", "无法开始或保存本次专注"},
	debugPrepareFailed:       {debugError, "preparation", "环境准备未完成"},
	debugStorageFailed:       {debugError, "storage", "无法保存本轮环境恢复记录"},
	debugAccessDenied:        {debugError, "permission", "当前归径未获辅助功能权限"},
	debugAccessFailed:        {debugError, "permission", "无法读取辅助功能权限"},
	debugAccessGranted:       {debugInfo, "permission", "已确认辅助功能授权"},
	debugTargetFailed:        {debugError, "window", "未找到可核对的工作应用或窗口"},
	debugWindowReady:         {debugInfo, "window", "已确认目标工作窗口"},
	debugWindowUnsupported:   {debugError, "window", "目标窗口不支持所需的原生全屏操作或观察"},
	debugFullscreenPreserved: {debugInfo, "fullscreen", "保留原有全屏"},
	debugFullscreenRequest:   {debugInfo, "fullscreen", "尝试进入原生全屏"},
	debugFullscreenFailed:    {debugError, "fullscreen", "未能确认目标窗口进入或保持原生全屏"},
	debugFullscreenConfirmed: {debugInfo, "fullscreen", "已确认原生全屏"},
	debugHandoffFailed:       {debugError, "handoff", "未能将前台交给工作窗口"},
	debugHandoffConfirmed:    {debugInfo, "handoff", "已交接到全屏工作窗口"},
	debugWindowedHandoff:     {debugInfo, "handoff", "已交接到工作应用，未要求窗口全屏"},
	debugRestoreFailed:       {debugWarn, "restore", "未能确认恢复，保留不确定的窗口状态"},
	debugRestoreConfirmed:    {debugInfo, "restore", "已完成本轮全屏恢复核对"},
}

const debugFileLimit = 256 * 1024

var debugBundleID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,254}$`)
var debugAttemptID = regexp.MustCompile(`^[A-Z2-7]{26}$`)

type debugRecord struct {
	At      time.Time  `json:"at"`
	Level   debugLevel `json:"level"`
	Stage   string     `json:"stage"`
	Code    debugCode  `json:"code"`
	Version string     `json:"version"`
	Build   string     `json:"build"`
	Attempt string     `json:"attempt,omitempty"`
	Target  string     `json:"target,omitempty"`
	AXCode  int32      `json:"axCode,omitempty"`
}

type debugRecorder struct {
	mu          sync.Mutex
	dir         string
	enabled     bool
	writeFailed bool
}

func (a *application) debugEvent(code debugCode, attempt, target string, axCode int32) {
	if a.debug != nil {
		a.debug.record(code, attempt, target, axCode)
	}
}

func (a *application) checkImmersionAccessFor(attempt string) error {
	err := a.checkImmersionAccess()
	code := debugAccessGranted
	if errors.Is(err, errImmersionPermission) {
		code = debugAccessDenied
	} else if err != nil {
		code = debugAccessFailed
	}
	a.debugEvent(code, attempt, "", 0)
	return err
}

func newDebugAttempt() string { return rand.Text() }

func (r *debugRecorder) setEnabled(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = on
	if on {
		r.append(debugEnabled, "", "", 0)
	}
}

func (r *debugRecorder) record(code debugCode, attempt, target string, axCode int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.enabled {
		r.append(code, attempt, target, axCode)
	}
}

func (r *debugRecorder) append(code debugCode, attempt, target string, axCode int32) {
	desc, ok := debugDescriptions[code]
	if !ok {
		return
	}
	if !debugBundleID.MatchString(target) {
		target = ""
	}
	if !debugAttemptID.MatchString(attempt) {
		attempt = ""
	}
	build := buildNumber
	if build == "" {
		build = buildID
	}
	v := debugRecord{At: time.Now().UTC(), Level: desc.level, Stage: desc.stage, Code: code,
		Version: buildVersion, Build: build, Attempt: attempt, Target: target, AXCode: axCode}
	b, err := json.Marshal(v)
	if err == nil {
		err = r.appendLine(append(b, '\n'))
	}
	r.writeFailed = r.writeFailed || err != nil
}

func (r *debugRecorder) appendLine(line []byte) error {
	path := filepath.Join(r.dir, "debug.jsonl")
	if len(line) > debugFileLimit {
		return errors.New("debug record exceeds file limit")
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return errors.New("invalid local debug file")
		}
		if info.Size()+int64(len(line)) > debugFileLimit {
			if err := os.Rename(path, path+".previous"); err != nil {
				return err
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(line)
	return errors.Join(err, f.Close())
}

func (r *debugRecorder) snapshot() ([]debugRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.dir, "debug.jsonl")
	var records []debugRecord
	for _, file := range []string{path + ".previous", path} {
		entries, err := readDebugRecords(file)
		if err != nil {
			return nil, r.writeFailed, err
		}
		records = append(records, entries...)
	}
	return records, r.writeFailed, nil
}

func readDebugRecords(path string) ([]debugRecord, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > debugFileLimit {
		return nil, errors.New("invalid local debug file")
	}
	var records []debugRecord
	s := bufio.NewScanner(io.LimitReader(f, debugFileLimit))
	for s.Scan() {
		var rec debugRecord
		if err := json.Unmarshal(s.Bytes(), &rec); err != nil {
			return nil, err
		}
		desc, ok := debugDescriptions[rec.Code]
		if !ok || rec.Level != desc.level || rec.Stage != desc.stage || rec.At.IsZero() ||
			!debugBundleID.MatchString(rec.Version) || !debugBundleID.MatchString(rec.Build) ||
			(rec.Target != "" && !debugBundleID.MatchString(rec.Target)) ||
			(rec.Attempt != "" && !debugAttemptID.MatchString(rec.Attempt)) {
			return nil, errors.New("invalid debug record")
		}
		records = append(records, rec)
	}
	return records, s.Err()
}

// The viewer and export use the same rendered snapshot.
func formatDebugRecords(records []debugRecord, level debugLevel) (string, int) {
	var b strings.Builder
	count := 0
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if level != "" && rec.Level != level {
			continue
		}
		count++
		fmt.Fprintf(&b, "%s  %s  %s  [%s/%s]  %s build %s",
			rec.At.Local().Format("2006-01-02 15:04:05 -0700"), rec.Level, debugDescriptions[rec.Code].text,
			rec.Stage, rec.Code, rec.Version, rec.Build)
		if rec.Attempt != "" {
			fmt.Fprintf(&b, "  尝试 %s", rec.Attempt)
		}
		if rec.Target != "" {
			fmt.Fprintf(&b, "  应用 %s", rec.Target)
		}
		if rec.AXCode != 0 {
			fmt.Fprintf(&b, "  AX %d", rec.AXCode)
		}
		b.WriteByte('\n')
	}
	return b.String(), count
}

func exportDebugLog(dataDir, path, text string) error {
	dir, err := os.Stat(dataDir)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return err
	}
	for ancestor := parent; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Stat(ancestor)
		if err != nil {
			return err
		}
		if os.SameFile(dir, info) {
			return errors.New("debug export cannot overwrite application data")
		}
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	return journal.WriteFile(filepath.Join(parent, filepath.Base(path)), []byte(text))
}
