package menu

import "testing"

func itemLabels(m *Menu) []string {
	labels := make([]string, len(m.Items))
	for i, it := range m.Items {
		labels[i] = it.Label
	}
	return labels
}

func selectLabel(t *testing.T, m *Menu, label string) {
	t.Helper()
	for i, it := range m.Items {
		if it.Label == label {
			m.SelectedIndex = i
			m.Select()
			return
		}
	}
	t.Fatalf("label %q not found in %v", label, itemLabels(m))
}

func TestMainMenuIsCategoryList(t *testing.T) {
	m := NewMenu()
	m.Open()
	want := []string{"Basic Settings...", "Appearance...", "AI Features...", "Web Features...", "Experimental Features...", "Save and Close"}
	labels := itemLabels(m)
	have := map[string]bool{}
	for _, l := range labels {
		have[l] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("main menu missing %q (have %v)", w, labels)
		}
	}
}

func TestCategoryNavigationAndBack(t *testing.T) {
	cases := []struct {
		entry string
		state MenuState
		item  string // an item expected inside the category
	}{
		{"Basic Settings...", MenuBasic, "Source RC Files"},
		{"Appearance...", MenuAppearance, "Cursor Blink"},
		{"AI Features...", MenuAI, "AI Tools (read-only)"},
		{"Web Features...", MenuWeb, "Web Search"},
		{"Experimental Features...", MenuExperimental, "Undercurl"},
	}
	for _, c := range cases {
		m := NewMenu()
		m.Open()
		selectLabel(t, m, c.entry)
		if m.State != c.state {
			t.Errorf("%s: state = %v, want %v", c.entry, m.State, c.state)
			continue
		}
		found := false
		for _, l := range itemLabels(m) {
			if l == c.item {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing item %q (have %v)", c.entry, c.item, itemLabels(m))
		}
		// Back returns to the main category list.
		selectLabel(t, m, "Back")
		if m.State != MenuMain {
			t.Errorf("%s: Back led to %v, want MenuMain", c.entry, m.State)
		}
	}
}

func TestToggleRebuildsInPlace(t *testing.T) {
	m := NewMenu()
	m.Open()
	selectLabel(t, m, "AI Features...")
	before := m.Config.Ollama.Tools
	selectLabel(t, m, "AI Tools (read-only)")
	if m.Config.Ollama.Tools == before {
		t.Error("toggle did not flip AI Tools")
	}
	if m.State != MenuAI {
		t.Errorf("toggle left state %v, want MenuAI", m.State)
	}
	// The rebuilt items must still be the AI menu, not the main menu.
	found := false
	for _, l := range itemLabels(m) {
		if l == "Ollama Chat" {
			found = true
		}
	}
	if !found {
		t.Errorf("after toggle, items are not the AI menu: %v", itemLabels(m))
	}
	// Restore the original value so the shared config file isn't affected
	// (toggles only persist on Save, but keep the in-memory copy clean too).
	m.Config.Ollama.Tools = before
}

func TestSubSubmenuEscapeReturnsToCategory(t *testing.T) {
	m := NewMenu()
	m.Open()
	selectLabel(t, m, "Appearance...")
	// Theme selector lives under Appearance...
	for i, it := range m.Items {
		if len(it.Label) >= 6 && it.Label[:6] == "Theme:" {
			m.SelectedIndex = i
			m.Select()
			break
		}
	}
	if m.State != MenuThemeSelect {
		t.Fatalf("expected MenuThemeSelect, got %v", m.State)
	}
	// ...so escape must return to Appearance, not the main list.
	m.HandleEscape()
	if m.State != MenuAppearance {
		t.Errorf("escape from theme select led to %v, want MenuAppearance", m.State)
	}
}

// Editing a value mid-string: caret moves, inserts, and deletes land where the
// caret is, not at the end of the buffer.
func TestInputCaretEditing(t *testing.T) {
	m := NewMenu()
	m.startInputWithValue(InputCommandValue, "Command to run:", "git status")
	if m.InputCursor != 10 {
		t.Fatalf("caret starts at %d, want end (10)", m.InputCursor)
	}

	m.MoveInputHome()
	m.HandleChar('!')
	if m.InputBuffer != "!git status" {
		t.Errorf("insert at home = %q", m.InputBuffer)
	}

	m.MoveInputCursor(3) // after "!git"
	m.HandlePaste(" -C /tmp")
	if m.InputBuffer != "!git -C /tmp status" {
		t.Errorf("paste at caret = %q", m.InputBuffer)
	}

	m.HandleBackspace()
	if m.InputBuffer != "!git -C /tm status" {
		t.Errorf("backspace at caret = %q", m.InputBuffer)
	}
	m.HandleDelete() // forward-delete the space
	if m.InputBuffer != "!git -C /tmstatus" {
		t.Errorf("delete at caret = %q", m.InputBuffer)
	}

	// Caret cannot run off either end.
	m.MoveInputCursor(-999)
	m.HandleBackspace()
	m.MoveInputCursor(999)
	m.HandleDelete()
	if m.InputBuffer != "!git -C /tmstatus" {
		t.Errorf("edits past the ends changed the buffer: %q", m.InputBuffer)
	}
}

// Up/Down walk lines in a multi-line field and report false at the edges so the
// caller can fall back to home/end.
func TestInputLineNavigation(t *testing.T) {
	m := NewMenu()
	m.startInputWithValue(InputScriptInit, "Init script:", "one\ntwo\nthree")

	if m.MoveInputLine(1) {
		t.Error("moved down from the last line")
	}
	if !m.MoveInputLine(-1) {
		t.Fatal("could not move up")
	}
	if line, col := m.InputCursorLineCol(); line != 1 || col != 3 {
		t.Errorf("up from end of line 3 = line %d col %d, want 1/3", line, col)
	}
	if !m.MoveInputLine(-1) {
		t.Fatal("could not move up again")
	}
	if line, col := m.InputCursorLineCol(); line != 0 || col != 3 {
		t.Errorf("up again = line %d col %d, want 0/3", line, col)
	}
	if m.MoveInputLine(-1) {
		t.Error("moved up from the first line")
	}

	// Column clamps to a shorter target line.
	m.startInputWithValue(InputScriptInit, "Init script:", "a\nlonger line")
	m.MoveInputLine(-1)
	if line, col := m.InputCursorLineCol(); line != 0 || col != 1 {
		t.Errorf("clamped column = line %d col %d, want 0/1", line, col)
	}

	// Editing lands on the caret's line, not the end of the buffer.
	m.MoveInputHome()
	m.HandleChar('x')
	if m.InputBuffer != "xa\nlonger line" {
		t.Errorf("insert on first line = %q", m.InputBuffer)
	}
}
