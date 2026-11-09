//go:build darwin

package main

import (
	"errors"
	"fmt"
	"focus.local/zhuanzhuli/internal/notification"
	m "focus.local/zhuanzhuli/internal/platform/macos"
	"focus.local/zhuanzhuli/pkg/journal"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
)

// All lease access stays on the serial environment worker. Only projections are
// published to the main thread. Fixture effects must never touch system Focus.
func (a *application) focusDriver() m.FocusDriver {
	d := m.FocusDriver{}
	mygo.RunOnMain(func() { d.PID = m.ControlCenterPID() })
	d.NewCloser = func() (*m.PanelCloser, error) { return m.NewPanelCloser(d.PID, mygo.RunOnMain) }
	d.Enable = func(action func() error) error {
		var err error
		mygo.RunOnMain(func() {
			if !a.preparationAllowed() {
				err = errors.New("专注已暂停或结束，取消系统专注模式请求")
				return
			}
			err = action()
		})
		return err
	}
	return d
}
func (a *application) publishNotifications(message string) {
	pending := a.notifications.Pending()
	mygo.RunOnMain(func() {
		a.notificationNotice = message
		a.focusPending = pending
		for _, label := range a.notificationStatuses {
			if label != 0 {
				m.Text(label, message)
			}
		}
	})
}
func (a *application) protectNotifications(enabled bool, name string) error {
	if a.fixtureRoot != "" {
		return nil
	}
	if !enabled {
		a.publishNotifications("通知联动未开启 · 当前为部分保护")
		return nil
	}
	continuing := func() bool {
		var valid bool
		mygo.RunOnMain(func() { valid = a.preparationAllowed() })
		return valid
	}
	err := a.notifications.Acquire(a.focusDriver(), name, continuing)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notification linkage:", err)
		a.publishNotifications(notificationFailureCaption(err))
		if a.notifications.Pending() {
			return fmt.Errorf("未能确认本次通知联动：%w", err)
		}
		// Explicit partial consent is checked at every start/resume. A failure
		// without a possible OS mutation is allowed only under that consent.
		return nil
	}
	a.publishNotifications("已观察到指定系统专注模式 · 标准通知效果待验证")
	return nil
}
func (a *application) notificationCaption() string {
	if a.notificationNotice == "" {
		return "通知联动未开启 · 当前为部分保护"
	}
	return a.notificationNotice
}

func notificationFailureCaption(err error) string {
	return fmt.Sprintf("通知联动未完成：%v\n当前为部分保护，通知和声音仍可能出现。请在通知设置中核对。", err)
}

func notificationPermissionCaption(trusted bool, err error) string {
	if err != nil {
		return fmt.Sprintf("当前版本权限无法确认：%v。通知效果仍待验证。", err)
	}
	if !trusted {
		return "当前版本未获辅助功能权限。系统中的同名开关不代表此版本已获授权；通知和声音仍可能出现。"
	}
	return "当前版本已获辅助功能权限；系统模式是否可用及通知效果仍待验证。"
}

// Audit the real production permission refusal path without prompting, switching
// Focus, hiding other apps or creating a session. Granting AX remains a human action.
func (a *application) notificationCheck(panelCloseProbe bool) {
	trusted, err := m.FocusAccess()
	result := map[string]any{"mode": "existing-permission-only", "accessibilityTrusted": trusted,
		"requestedPermission": false, "switchedFocus": false, "notificationDeliveryVerified": false,
		"capabilityVerified": false, "productionRefusalChecked": false}
	if panelCloseProbe {
		result["mode"] = "bounded-panel-close-read-only"
		result["escapePairsSent"] = 0
		result["panelClosedVerified"] = false
	}
	if err != nil {
		result["error"] = err.Error()
	}
	if trusted {
		result["status"] = "system-ui-and-delivery-not-verified"
	} else {
		result["status"] = "permission-required"
	}
	a.effect(func() error {
		if trusted && err == nil {
			driver := a.focusDriver()
			var closer *m.PanelCloser
			if panelCloseProbe {
				factory := driver.NewCloser
				driver.NewCloser = func() (*m.PanelCloser, error) {
					var e error
					closer, e = factory()
					return closer, e
				}
			}
			e := driver.WithPanel(func(p notification.Panel) error {
				modes, e := p.Modes()
				if e == nil {
					result["observedModes"] = modes
					result["focusPanelVerified"] = true
				}
				return e
			})
			if closer != nil {
				result["escapePairsSent"] = closer.SentPairs
				result["focusedPIDAtClose"] = closer.FocusedPID
				result["panelClosedVerified"] = e == nil && closer.SentPairs == 1
			}
			return e
		}
		if !trusted && err == nil {
			// Never enter Acquire, even for a permission-refusal test: a grant
			// could change between reads. Neither diagnostic has a Set path.
			result["permissionRefused"] = true
			return errors.New("未授予当前构建辅助功能权限；只读通知探针未运行")
		}
		return nil
	}, func(e error) {
		if e != nil {
			result["error"] = e.Error()
		}
		result["notice"] = a.notificationCaption()
		result["pendingChanges"] = a.focusPending
		if e = journal.WriteJSON(filepath.Join(a.dir, "notification-check.json"), result); e != nil {
			fmt.Println(e)
		}
		a.quit()
	})
}
