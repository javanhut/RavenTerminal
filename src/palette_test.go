package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/keybindings"
)

func TestFuzzyScore(t *testing.T) {
	if _, ok := fuzzyScore("ntb", "New tab"); !ok {
		t.Fatal("subsequence must match")
	}
	if _, ok := fuzzyScore("xyz", "New tab"); ok {
		t.Fatal("non-subsequence must not match")
	}
	prefix, _ := fuzzyScore("split", "Split pane vertically")
	scattered, _ := fuzzyScore("split", "Show pane list in tab")
	if prefix <= scattered {
		t.Fatalf("contiguous word match %d must beat scattered %d", prefix, scattered)
	}
}

func TestPaletteEntriesAndFilter(t *testing.T) {
	entries := paletteEntries(config.ThemeOptions(), []fonts.FontInfo{{Name: "jetbrains", DisplayName: "JetBrains Mono"}})
	var haveTheme, haveFont, haveZoom bool
	for _, e := range entries {
		haveTheme = haveTheme || e.Theme == "ghostty"
		haveFont = haveFont || e.Font == "jetbrains"
		haveZoom = haveZoom || e.Action == keybindings.ActionTogglePaneZoom
	}
	if !haveTheme || !haveFont || !haveZoom {
		t.Fatalf("entries missing theme/font/action: %v %v %v", haveTheme, haveFont, haveZoom)
	}
	if got := filterPalette(entries, ""); len(got) != len(entries) {
		t.Fatal("empty query must list everything")
	}
	got := filterPalette(entries, "zoom pane")
	if len(got) == 0 || got[0].Action != keybindings.ActionTogglePaneZoom {
		t.Fatalf("'zoom pane' top = %+v", got)
	}
	got = filterPalette(entries, "ghostty")
	if len(got) == 0 || got[0].Theme != "ghostty" {
		t.Fatalf("'ghostty' top = %+v", got)
	}
}

func TestPaletteStateKeys(t *testing.T) {
	var p paletteState
	p.openWith(paletteEntries(nil, nil))
	for _, r := range "broadcast" {
		p.typeRune(r)
	}
	e, ok := p.choose()
	if !ok || e.Action != keybindings.ActionToggleBroadcast {
		t.Fatalf("choose = %+v %v", e, ok)
	}
	p.openWith(paletteEntries(nil, nil))
	p.typeRune('c')
	p.typeRune('o')
	p.typeRune('p')
	p.typeRune('y')
	second := p.shown[1]
	p.move(1)
	if e, _ := p.choose(); e.Label != second.Label {
		t.Fatalf("move(1) then choose = %q, want %q", e.Label, second.Label)
	}
	p.move(-5)
	if p.sel != 0 {
		t.Fatal("selection must clamp at the top")
	}
	p.backspace()
	if p.query != "cop" {
		t.Fatalf("backspace -> %q", p.query)
	}
	p.openWith(nil)
	if _, ok := p.choose(); ok {
		t.Fatal("empty list chooses nothing")
	}
}
