//go:build darwin

package macos

import "errors"

func ApplyAppearance(value string) error {
	name := "NSAppearanceNameAqua"
	if value == "dark" {
		name = "NSAppearanceNameDarkAqua"
	} else if value != "light" {
		return errors.New("外观选项无效")
	}
	appearance := Send(Class("NSAppearance"), "appearanceNamed:", NS(name))
	if appearance == 0 {
		return errors.New("此版本无法应用所选外观")
	}
	Send(Send(Class("NSApplication"), "sharedApplication"), "setAppearance:", appearance)
	return nil
}
