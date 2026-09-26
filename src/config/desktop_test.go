package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDesktopAppearance(t *testing.T) {
	d := ParseDesktopAppearance("[appearance]\naccent = \"#F7768E\"\ntheme_mode = \"light\"\ntransparency = false\n")
	if !d.IsLight() || d.AccentHex() != "#F7768E" {
		t.Fatalf("got %+v", d)
	}
	// A bad accent falls back; unknown keys and sections are ignored.
	d = ParseDesktopAppearance("[privacy]\nx = 1\n[appearance]\naccent = \"red\"\nblur = true\n")
	if d.IsLight() || d.AccentHex() != DefaultDesktopAccent {
		t.Fatalf("got %+v", d)
	}
	// Auto is dark; a parse error means the defaults.
	if ParseDesktopAppearance("[appearance]\ntheme_mode = \"auto\"\n").IsLight() {
		t.Error("auto should be dark")
	}
	d = ParseDesktopAppearance("[appearance\ntheme_mode = \"light\"")
	if d.IsLight() || d.AccentHex() != DefaultDesktopAccent {
		t.Errorf("parse error should give defaults, got %+v", d)
	}
}

func TestDesktopConfigPathHonoursXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got, want := DesktopConfigPath(), filepath.Join(dir, "raven", "desktop.toml"); got != want {
		t.Fatalf("DesktopConfigPath() = %q, want %q", got, want)
	}
	if err := os.MkdirAll(filepath.Join(dir, "raven"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DesktopConfigPath(), []byte("[appearance]\ntheme_mode = \"light\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !LoadDesktopAppearance().IsLight() {
		t.Error("LoadDesktopAppearance should read the XDG file")
	}
}

func TestDesktopStatChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.toml")
	_, missErr := os.Stat(path)
	if desktopStatChanged(nil, missErr, nil, missErr) {
		t.Error("still missing is not a change")
	}
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !desktopStatChanged(nil, missErr, a, nil) {
		t.Error("creation should be a change")
	}
	if desktopStatChanged(a, nil, a, nil) {
		t.Error("same stat is not a change")
	}
	// Rename-over (how Settings writes) replaces the file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("bb"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.Stat(path)
	if !desktopStatChanged(a, nil, b, nil) {
		t.Error("rename-over should be a change")
	}
}

func TestDefaultThemeFollowsDesktop(t *testing.T) {
	if got := DefaultConfig().Theme; got != "raven" {
		t.Fatalf("default theme = %q, want raven", got)
	}
	if ThemeOptions()[0].Name != "raven" {
		t.Error("raven should be the first theme option")
	}
}
