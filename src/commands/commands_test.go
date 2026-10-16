package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/keybindings"
)

func TestKeybindingsHelpUsesLeaderChords(t *testing.T) {
	h := KeybindingsHelp()
	for _, want := range []string{"Leader+Q", "Leader+R", "Leader+Enter", "F11", "Shift+PageUp", "Leader+T", "change-font <name>"} {
		if !strings.Contains(h, want) {
			t.Errorf("help missing %q", want)
		}
	}
	for _, stale := range []string{"Ctrl+Q ", "Shift+Enter     Toggle", "Ctrl+R "} {
		if strings.Contains(h, stale) {
			t.Errorf("help still advertises %q", stale)
		}
	}
}

func TestFontListMarksCurrent(t *testing.T) {
	out := FontList([]fonts.FontInfo{{Name: "a", DisplayName: "Font A"}, {Name: "b", DisplayName: "Font B"}}, "b")
	if !strings.Contains(out, "  a (Font A)\n") || !strings.Contains(out, "> b (Font B)\n") {
		t.Fatalf("font list = %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Fatal("font list must be plain \\n text for stdout")
	}
}

type fakeFonts struct{ got string }

func (f *fakeFonts) ChangeFont(name string) error {
	if name != "firacode" {
		return errors.New("font '" + name + "' not found")
	}
	f.got = name
	return nil
}
func (f *fakeFonts) CurrentFont() string { return f.got }
func (f *fakeFonts) GetAvailableFonts() []fonts.FontInfo {
	return []fonts.FontInfo{{Name: "firacode"}, {Name: "jetbrains"}}
}

func TestChangeFont(t *testing.T) {
	f := &fakeFonts{}
	if msg, err := ChangeFont(f, "  FiraCode "); err != nil || f.got != "firacode" || !strings.Contains(msg, "firacode") {
		t.Fatalf("ChangeFont ok = %q %v got=%q", msg, err, f.got)
	}
	msg, err := ChangeFont(f, "nope")
	if err == nil || !strings.Contains(msg, "firacode, jetbrains") {
		t.Fatalf("ChangeFont bad = %q %v", msg, err)
	}
	if msg, err := ChangeFont(f, ""); err != nil || !strings.Contains(msg, "> firacode") {
		t.Fatalf("ChangeFont empty lists fonts = %q %v", msg, err)
	}
}

func TestKeybindingsHelpListsEveryAction(t *testing.T) {
	h := KeybindingsHelp()
	for _, a := range keybindings.Actions() {
		if !strings.Contains(h, a.Name) || !strings.Contains(h, a.Chord) {
			t.Errorf("help lacks %q (%s)", a.Name, a.Chord)
		}
	}
	for _, want := range []string{"Leader+P", "Command palette", "Middle-click", "Alt+drag", "Leader+click"} {
		if !strings.Contains(h, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	for _, stale := range []string{"Ctrl+Tab ", "Ctrl+Shift+P on Linux", "Ctrl+Shift+V on Linux"} {
		if strings.Contains(h, stale) {
			t.Errorf("help still advertises %q", stale)
		}
	}
}

func TestChangeFontSkipsCurrentFont(t *testing.T) {
	f := &countingFonts{fakeFonts: fakeFonts{got: "firacode"}}
	if _, err := ChangeFont(f, " FiraCode "); err != nil {
		t.Fatal(err)
	}
	if f.loads != 0 {
		t.Fatalf("current font reloaded %d times", f.loads)
	}
}

type countingFonts struct {
	fakeFonts
	loads int
}

func (c *countingFonts) ChangeFont(name string) error {
	c.loads++
	return c.fakeFonts.ChangeFont(name)
}
