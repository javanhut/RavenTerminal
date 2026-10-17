package menu

import "testing"

func TestNewSettingsEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	m := NewMenu()
	m.Open()
	selectLabel(t, m, "Appearance...")
	for _, toggle := range []struct {
		label string
		get   func() bool
	}{
		{"Ligatures", func() bool { return m.Config.Appearance.Ligatures }},
		{"Prompt Marks Gutter", func() bool { return m.Config.Appearance.MarksGutter }},
		{"Reduced Motion", func() bool { return m.Config.Appearance.ReducedMotion }},
	} {
		before := toggle.get()
		selectLabel(t, m, toggle.label)
		if toggle.get() == before || m.State != MenuAppearance {
			t.Errorf("%s: toggled=%v state=%v", toggle.label, toggle.get() != before, m.State)
		}
	}

	selectLabel(t, m, "Scrollback Lines: 10000")
	if !m.InputActive || m.InputState != InputScrollback {
		t.Fatalf("scrollback input not started: %v %v", m.InputActive, m.InputState)
	}
	m.InputBuffer = "50000"
	m.HandleEnter()
	if m.Config.ScrollbackLines != 50000 {
		t.Fatalf("scrollback = %d", m.Config.ScrollbackLines)
	}
	selectLabel(t, m, "Scrollback Lines: 50000")
	m.InputBuffer = "nope"
	m.HandleEnter()
	if m.Config.ScrollbackLines != 50000 {
		t.Fatalf("invalid input changed scrollback to %d", m.Config.ScrollbackLines)
	}

	selectLabel(t, m, "Background Opacity: 100%")
	m.InputBuffer = "85"
	m.HandleEnter()
	if m.Config.Appearance.BackgroundOpacity != 0.85 {
		t.Fatalf("opacity = %v", m.Config.Appearance.BackgroundOpacity)
	}
	selectLabel(t, m, "Back")

	selectLabel(t, m, "Basic Settings...")
	selectLabel(t, m, "Bell: visual")
	selectLabel(t, m, "Bell: attention")
	selectLabel(t, m, "Bell: none")
	if m.Config.Bell != "visual" {
		t.Fatalf("bell cycle ended at %q", m.Config.Bell)
	}
	before := m.Config.Notifications
	selectLabel(t, m, "Desktop Notifications")
	if m.Config.Notifications == before {
		t.Fatal("notifications toggle did not flip")
	}
	selectLabel(t, m, "Notify Commands After: 10s")
	m.InputBuffer = "30"
	m.HandleEnter()
	if m.Config.NotifyCommandAfter != 30 {
		t.Fatalf("notify after = %d", m.Config.NotifyCommandAfter)
	}
}

func TestBasicMenuHomeSetting(t *testing.T) {
	m := NewMenu()
	m.Open()
	selectLabel(t, m, "Basic Settings...")
	selectLabel(t, m, "Home: (inherited $HOME)")
	m.InputBuffer = "  ~/profiles/work "
	m.HandleEnter()
	if m.Config.Shell.Home != "~/profiles/work" {
		t.Fatalf("home = %q", m.Config.Shell.Home)
	}
	selectLabel(t, m, "Home: ~/profiles/work")
	m.InputBuffer = ""
	m.HandleEnter()
	if m.Config.Shell.Home != "" {
		t.Fatalf("clearing home left %q", m.Config.Shell.Home)
	}
	selectLabel(t, m, "Home: (inherited $HOME)")
}

func TestBasicMenuWindowOpsToggle(t *testing.T) {
	m := NewMenu()
	m.Open()
	selectLabel(t, m, "Basic Settings...")
	if m.Config.AllowWindowOps {
		t.Fatal("window ops on by default")
	}
	selectLabel(t, m, "Let Apps Resize the Window")
	if !m.Config.AllowWindowOps {
		t.Fatal("toggle did not enable window ops")
	}
}
