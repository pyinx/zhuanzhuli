//go:build darwin

package main

import (
	"errors"
	"focus.local/zhuanzhuli/internal/session"
	"focus.local/zhuanzhuli/internal/store"
	"strings"
	"testing"
	"time"
)

func TestNotificationFailureExplainsActualCauseAndRemainingExposure(t *testing.T) {
	for _, cause := range []string{"未授予当前版本辅助功能权限", "找不到模式“工作”", "控制中心正在使用或状态未知"} {
		t.Run(cause, func(t *testing.T) {
			caption := notificationFailureCaption(errors.New(cause))
			if !strings.Contains(caption, cause) || !strings.Contains(caption, "部分保护") || !strings.Contains(caption, "通知和声音仍可能出现") {
				t.Fatalf("missing actionable failure or remaining exposure: %q", caption)
			}
		})
	}
}

// Exercise the production transitions while an environment operation is in
// flight. No UI runtime, system Focus driver or permission grant is involved.
func cleanupTransitionApp(t *testing.T) *application {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Data.Preferences.Apps = []store.App{{ID: "local.review.fixture", Name: "Fixture", Path: "/Applications/Fixture.app"}}
	c := session.New(time.Now)
	if err := c.Start(session.State{ID: "review-transition", Goal: "收尾转移", Minutes: 30, Scene: "forest", Allowed: []string{"local.review.fixture"}, Partial: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Enter(); err != nil {
		t.Fatal(err)
	}
	return &application{store: s, controller: c, busy: true, focusPending: true}
}

func TestUserPauseQueuesEnvironmentCleanup(t *testing.T) {
	a := cleanupTransitionApp(t)
	a.pauseByUser("用户暂停")
	if a.controller.Snapshot().Phase != "paused" || !a.needsRestore || a.store.Data.Current == nil || a.store.Data.Current.Phase != "paused" {
		t.Fatal("user pause did not stop and persist before queued cleanup")
	}
}

func TestSystemPauseCancelsQueuedQuit(t *testing.T) {
	a := cleanupTransitionApp(t)
	a.quit()
	if !a.quitting {
		t.Fatal("quit intent was not queued")
	}
	a.pauseForSystem("屏幕锁定")
	if a.quitting || a.controller.Snapshot().Phase != "paused" || !a.needsRestore {
		t.Fatal("system pause retained a queued quit or discarded pending environment cleanup")
	}
}

func TestNotificationPermissionDoesNotPromiseNotificationProtection(t *testing.T) {
	for _, state := range []struct {
		name    string
		trusted bool
		err     error
		want    string
	}{
		{"missing", false, nil, "当前版本未获辅助功能权限"},
		{"granted", true, nil, "通知效果仍待验证"},
		{"unknown", true, errors.New("系统查询失败"), "权限无法确认：系统查询失败"},
	} {
		t.Run(state.name, func(t *testing.T) {
			caption := notificationPermissionCaption(state.trusted, state.err)
			if !strings.Contains(caption, state.want) || strings.Contains(caption, "通知已屏蔽") {
				t.Fatalf("permission was confused with verified protection: %q", caption)
			}
		})
	}
}
