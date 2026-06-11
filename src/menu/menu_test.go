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
