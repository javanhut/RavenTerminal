package render

import "testing"

// TestShouldRedraw exercises the redraw policy: no trigger => no frame; any
// single trigger => frame. Table-driven over every field so a newly added
// trigger that isn't wired into ShouldRedraw fails here.
func TestShouldRedraw(t *testing.T) {
	if ShouldRedraw(RedrawTriggers{}) {
		t.Fatal("no triggers: ShouldRedraw = true, want false (idle must reach zero draws)")
	}

	cases := []struct {
		name string
		set  func(*RedrawTriggers)
	}{
		{"PaneContentDirty", func(r *RedrawTriggers) { r.PaneContentDirty = true }},
		{"GridSwapped", func(r *RedrawTriggers) { r.GridSwapped = true }},
		{"ActiveTabChanged", func(r *RedrawTriggers) { r.ActiveTabChanged = true }},
		{"CursorPhaseChanged", func(r *RedrawTriggers) { r.CursorPhaseChanged = true }},
		{"SelectionDragging", func(r *RedrawTriggers) { r.SelectionDragging = true }},
		{"ToastVisible", func(r *RedrawTriggers) { r.ToastVisible = true }},
		{"ToastJustExpired", func(r *RedrawTriggers) { r.ToastJustExpired = true }},
		{"MenuOpen", func(r *RedrawTriggers) { r.MenuOpen = true }},
		{"SearchPanelOpen", func(r *RedrawTriggers) { r.SearchPanelOpen = true }},
		{"AIPanelOpen", func(r *RedrawTriggers) { r.AIPanelOpen = true }},
		{"HelpOpen", func(r *RedrawTriggers) { r.HelpOpen = true }},
		{"SizeChanged", func(r *RedrawTriggers) { r.SizeChanged = true }},
		{"FocusChanged", func(r *RedrawTriggers) { r.FocusChanged = true }},
		{"ScaleChanged", func(r *RedrawTriggers) { r.ScaleChanged = true }},
		{"SyncActiveOrEnded", func(r *RedrawTriggers) { r.SyncActiveOrEnded = true }},
		{"UIStateChanged", func(r *RedrawTriggers) { r.UIStateChanged = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var trig RedrawTriggers
			tc.set(&trig)
			if !ShouldRedraw(trig) {
				t.Fatalf("%s alone: ShouldRedraw = false, want true", tc.name)
			}
		})
	}
}
