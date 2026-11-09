//go:build darwin

package macos

import (
	"fmt"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func focusAuthorization(code int64) string {
	switch code {
	case 0:
		return "not-determined"
	case 1:
		return "restricted"
	case 2:
		return "denied"
	case 3:
		return "authorized"
	default:
		return "unknown"
	}
}
func notificationAuthorization(code int64) string {
	switch code {
	case 0:
		return "not-determined"
	case 1:
		return "denied"
	case 2:
		return "authorized"
	case 3:
		return "provisional"
	default:
		return "unknown"
	}
}
func notificationSetting(code int64) string {
	switch code {
	case 0:
		return "not-supported"
	case 1:
		return "disabled"
	case 2:
		return "enabled"
	default:
		return "unknown"
	}
}

// RequestFixtureNotificationPermission is used only by the owned fixture's
// explicit send action. Preserve the raw authorization callback: MyGo's Show
// currently drops this result, so a send error cannot establish a user's choice.
func RequestFixtureNotificationPermission(done func(bool, error)) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		done(false, err)
		return
	}
	center := send(objc.ID(objc.GetClass("UNUserNotificationCenter")), "currentNotificationCenter")
	if center == 0 {
		done(false, fmt.Errorf("notification center unavailable"))
		return
	}
	block := objc.NewBlock(func(_ objc.Block, granted bool, nsErr objc.ID) {
		if nsErr != 0 {
			done(granted, fmt.Errorf("%s (%d): %s", stringOf(send(nsErr, "domain")), objc.Send[int64](nsErr, sel("code")), stringOf(send(nsErr, "localizedDescription"))))
			return
		}
		done(granted, nil)
	})
	send(center, "requestAuthorizationWithOptions:completionHandler:", uint64(1<<1|1<<2), uintptr(block))
	block.Release()
}

// NotificationFacts reads this bundle's settings. It never requests permission,
// sends a notification, changes Focus, or accesses another app's notifications.
// Called on the main thread in a real app bundle; done may run on another queue.
func NotificationFacts(done func(map[string]any, error)) {
	if err := Init(); err != nil {
		done(nil, err)
		return
	}
	for _, framework := range []string{"UserNotifications", "Intents", "ApplicationServices"} {
		if _, err := purego.Dlopen("/System/Library/Frameworks/"+framework+".framework/"+framework, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			done(nil, err)
			return
		}
	}
	facts := map[string]any{"scope": "current-app", "mode": "read-only", "focus": map[string]any{"status": "unknown"}}
	focus := facts["focus"].(map[string]any)
	if c := objc.GetClass("INFocusStatusCenter"); c != 0 {
		center := send(objc.ID(c), "defaultCenter")
		if center != 0 {
			code := objc.Send[int64](center, sel("authorizationStatus"))
			focus["authorizationCode"] = code
			focus["authorization"] = focusAuthorization(code)
			focus["hasFocusStatusSetter"] = objc.Send[bool](center, sel("respondsToSelector:"), sel("setFocusStatus:"))
			focus["isFocused"] = nil
			if code == 3 {
				value := send(send(center, "focusStatus"), "isFocused")
				if value != 0 {
					focus["isFocused"] = objc.Send[bool](value, sel("boolValue"))
					focus["status"] = "observed"
				}
			}
		}
	}
	lib, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		done(nil, err)
		return
	}
	var trusted func() bool
	purego.RegisterLibFunc(&trusted, lib, "AXIsProcessTrusted")
	facts["accessibilityTrustedForThisProcess"] = trusted()
	center := send(objc.ID(objc.GetClass("UNUserNotificationCenter")), "currentNotificationCenter")
	if center == 0 {
		done(nil, fmt.Errorf("notification center unavailable"))
		return
	}
	block := objc.NewBlock(func(_ objc.Block, settings objc.ID) {
		if settings == 0 {
			done(nil, fmt.Errorf("notification settings unavailable"))
			return
		}
		code := objc.Send[int64](settings, sel("authorizationStatus"))
		notes := map[string]any{"authorizationCode": code, "authorization": notificationAuthorization(code)}
		for _, property := range []string{"alertSetting", "soundSetting", "badgeSetting", "notificationCenterSetting", "lockScreenSetting", "criticalAlertSetting", "timeSensitiveSetting", "scheduledDeliverySetting", "directMessagesSetting"} {
			if objc.Send[bool](settings, sel("respondsToSelector:"), sel(property)) {
				value := objc.Send[int64](settings, sel(property))
				notes[property] = map[string]any{"code": value, "name": notificationSetting(value)}
			} else {
				notes[property] = map[string]any{"name": "unknown", "reason": "selector unavailable on running OS"}
			}
		}
		facts["notifications"] = notes
		done(facts, nil)
	})
	send(center, "getNotificationSettingsWithCompletionHandler:", uintptr(block))
	block.Release()
}
