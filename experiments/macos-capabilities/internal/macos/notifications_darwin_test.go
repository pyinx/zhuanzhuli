//go:build darwin

package macos

import "testing"

func TestAuthorizationEnumsDoNotConfuseFocusAndNotificationPermission(t *testing.T) {
	if focusAuthorization(1) != "restricted" || notificationAuthorization(1) != "denied" {
		t.Fatal("distinct Apple enums conflated")
	}
	if focusAuthorization(2) != "denied" || notificationAuthorization(2) != "authorized" {
		t.Fatal("notification permission mistaken for Focus authorization")
	}
	if notificationAuthorization(0) != "not-determined" || notificationSetting(0) != "not-supported" {
		t.Fatal("zero incorrectly interpreted as disabled or protected")
	}
	for _, f := range []func(int64) string{focusAuthorization, notificationAuthorization, notificationSetting} {
		if f(99) != "unknown" {
			t.Fatal("unrecognized value must remain unknown")
		}
	}
}
