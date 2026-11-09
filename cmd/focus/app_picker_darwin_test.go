//go:build darwin

package main

import (
	"focus.local/zhuanzhuli/internal/store"
	"testing"
)

func TestSameNamedApplicationSelectionHasStableIdentityOrder(t *testing.T) {
	selected := map[string]store.App{"a": {ID: "a", Name: "Editor"}, "b": {ID: "b", Name: "Editor"}}
	for i := 0; i < 1000; i++ {
		items := sortedAppSelection(selected)
		if items[0].ID != "a" || items[1].ID != "b" {
			t.Fatal("same-name app row identity changed", items)
		}
	}
}
