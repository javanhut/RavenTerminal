package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/tab"
)

type focusEvent struct {
	pane    *tab.Pane
	focused bool
}

func TestFocusReportsFollowActivePane(t *testing.T) {
	uxIsolateHome(t)
	tm, err := tab.NewTabManagerIn(80, 24, "", []string{"cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tm.CloseAll)
	first := activePane(tm.ActiveTab())
	if err := tm.NewTabWithCommand([]string{"cat"}, ""); err != nil {
		t.Fatal(err)
	}
	second := activePane(tm.ActiveTab())

	var got []focusEvent
	old := writePaneFocus
	writePaneFocus = func(p *tab.Pane, focused bool) { got = append(got, focusEvent{p, focused}) }
	t.Cleanup(func() { writePaneFocus = old })

	a := &App{tabManager: tm, windowFocused: true}
	step := func(name string, want ...focusEvent) {
		t.Helper()
		got = nil
		a.syncFocus()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", name, got, want)
			}
		}
	}
	step("start", focusEvent{second, true})
	step("no change")
	tm.SelectTab(0)
	step("tab switch", focusEvent{second, false}, focusEvent{first, true})
	a.windowFocused = false
	step("window blur", focusEvent{first, false})
	a.windowFocused = true
	step("window focus", focusEvent{first, true})
}
