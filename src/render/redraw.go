package render

// RedrawTriggers is the SINGLE enumeration of every condition that forces a
// frame to be rendered and presented. The main loop fills one of these per
// wake and skips all GL work (render + SwapBuffers) when ShouldRedraw returns
// false, so an idle terminal with cursor blink off reaches zero draws between
// events. Miss a trigger here and the UI looks frozen for that state change —
// add the field AND wire it in both ShouldRedraw and the main loop.
//
// Triggers:
//   - PaneContentDirty:   a visible pane's grid changed (content bytes, cursor
//     position, scrollback offset, or selection) since its last snapshot
//   - GridSwapped:        a pane's active grid pointer changed (alternate
//     screen enter/exit) or a new/unseen pane appeared (splits)
//   - ActiveTabChanged:   a different tab became active
//   - CursorPhaseChanged: the drawn-cursor on/off state flipped (blink phase,
//     DECTCEM visibility, or blink pausing on focus/typing)
//   - SelectionDragging:  a mouse selection drag is in progress (edge
//     auto-scroll repaints)
//   - TabDragging:        a tab-bar drag-reorder is in progress (the reordered
//     chips must repaint even when nothing else is dirty)
//   - ToastVisible:       a toast overlay is showing (it renders with a
//     time-based fade/expiry)
//   - ToastJustExpired:   the toast expired since the last frame (one repaint
//     to erase it)
//   - MenuOpen/SearchPanelOpen/AIPanelOpen/HelpOpen: an overlay panel is open
//     (panels stream/animate content outside the grid dirty tracking)
//   - SizeChanged:        the framebuffer size changed (resize)
//   - FocusChanged:       window focus changed (affects cursor rendering)
//   - ScaleChanged:       the monitor content scale changed (HiDPI move)
//   - SyncActiveOrEnded:  synchronized output (?2026) is holding presents, or
//     was released since the last frame (frames rendered during the hold are
//     not presented, so the release needs one more frame even if nothing is
//     dirty anymore)
//   - UIStateChanged:     renderer-side state changed (hover-URL underline,
//     theme, tab bar visibility, font size/zoom)
type RedrawTriggers struct {
	PaneContentDirty   bool
	GridSwapped        bool
	ActiveTabChanged   bool
	CursorPhaseChanged bool
	SelectionDragging  bool
	TabDragging        bool
	ToastVisible       bool
	ToastJustExpired   bool
	MenuOpen           bool
	SearchPanelOpen    bool
	AIPanelOpen        bool
	HelpOpen           bool
	SizeChanged        bool
	FocusChanged       bool
	ScaleChanged       bool
	SyncActiveOrEnded  bool
	UIStateChanged     bool
}

// ShouldRedraw is the redraw policy: render a frame iff any trigger fired.
// Pure function so the policy is unit-testable without a GL context.
func ShouldRedraw(t RedrawTriggers) bool {
	return t != RedrawTriggers{}
}
