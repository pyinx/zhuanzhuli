package main

import (
	"focus.local/macos-capabilities/internal/session"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestOutsideChoiceIsPresentedOnlyOnNewPendingTransition(t *testing.T) {
	if !needsChoice(session.View{}, session.View{PendingChoice: true}) {
		t.Fatal("first outside event must present a choice")
	}
	if needsChoice(session.View{PendingChoice: true}, session.View{PendingChoice: true}) {
		t.Fatal("repeat events would reopen the panel")
	}
	if needsChoice(session.View{}, session.View{}) {
		t.Fatal("spurious panel")
	}
}
func TestNativeChoiceProvidesAllThreeExplicitActions(t *testing.T) {
	for label, want := range map[string]string{"返回": "return", "加入本次": "allow", "退出专注": "end"} {
		selected := ""
		tt := ui.NewTester(func(c *ui.Context) { choiceView(c, false, func(kind string) { selected = kind }) }, 440, 180)
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if selected != want {
			t.Fatalf("%s selected %s", label, selected)
		}
	}
}
