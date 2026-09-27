package render

import (
	"math"
	"testing"

	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/grid"
)

func TestHexColor(t *testing.T) {
	if got := hexColor("#FF8000"); got != ([4]float32{1, float32(0x80) / 255, 0, 1}) {
		t.Fatalf("hexColor = %v", got)
	}
}

func TestRavenThemeDarkUsesAccent(t *testing.T) {
	th := RavenTheme(config.DesktopAppearance{ThemeMode: "auto", Accent: "#F7768E"})
	accent := hexColor("#F7768E")
	if th.Light || th.ANSI != nil {
		t.Fatal("auto should give the dark palette")
	}
	if th.Background != ThemeByName("raven-blue").Background {
		t.Error("dark raven should be based on raven-blue")
	}
	if th.Cursor != accent || th.TabActive != accent || th.Selection != withAlpha(accent, 0.35) {
		t.Errorf("accent not applied: %+v", th)
	}
	// An invalid accent falls back to the desktop default.
	th = RavenTheme(config.DesktopAppearance{Accent: "blue"})
	if th.TabActive != hexColor(config.DefaultDesktopAccent) {
		t.Errorf("fallback accent = %v", th.TabActive)
	}
}

// luminance is the WCAG relative luminance of an sRGB color.
func luminance(c [4]float32) float64 {
	lin := func(v float32) float64 {
		x := float64(v)
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

func TestRavenThemeLightIsReadable(t *testing.T) {
	th := RavenTheme(config.DesktopAppearance{ThemeMode: "light"})
	if !th.Light || th.ANSI == nil {
		t.Fatal("light should give the light palette with its own ANSI colors")
	}
	contrast := func(a, b [4]float32) float64 {
		la, lb := luminance(a), luminance(b)
		if la < lb {
			la, lb = lb, la
		}
		return (la + 0.05) / (lb + 0.05)
	}
	if c := contrast(th.Foreground, th.Background); c < 7 {
		t.Errorf("fg/bg contrast %.2f < 7", c)
	}
	// Every ANSI entry must be legible as text on the light background.
	for i, c := range th.ANSI {
		if got := contrast(c, th.Background); got < 2.5 {
			t.Errorf("ANSI %d contrast %.2f < 2.5", i, got)
		}
	}
	// The renderer uses the override for indices < 16 only.
	r := &Renderer{theme: th}
	if got := r.colorToRGBA(grid.IndexedColor(1), false); got != th.ANSI[1] {
		t.Errorf("indexed 1 = %v, want override", got)
	}
	if got := r.colorToRGBA(grid.IndexedColor(200), false); got != indexedColor(200) {
		t.Errorf("indexed 200 = %v, want cube color", got)
	}
}

func TestRavenThemeGlass(t *testing.T) {
	base := RavenTheme(config.DesktopAppearance{})
	for _, g := range []string{"", "black", "Black Glass", "sepia"} {
		th := RavenTheme(config.DesktopAppearance{GlassTheme: g})
		if th.Background != base.Background || th.UI != nil || th.ui() != &darkUI {
			t.Errorf("%q should leave the palette alone", g)
		}
	}
	th := RavenTheme(config.DesktopAppearance{GlassTheme: "Rose Glass", Accent: "#F7768E"})
	if th.Background != hexColor("#5A3A4E") || th.Foreground != hexColor("#FFF4F8") {
		t.Errorf("rose dark = %v / %v", th.Background, th.Foreground)
	}
	if th.Background[3] != base.Background[3] || th.ui().PanelBg[3] != darkUI.PanelBg[3] {
		t.Error("glass must keep the palette's alpha")
	}
	if th.ui().PanelBg != withAlpha(th.Background, darkUI.PanelBg[3]) || th.TabActive != hexColor("#F7768E") {
		t.Errorf("chrome/accent not applied: %+v", th)
	}
	th = RavenTheme(config.DesktopAppearance{ThemeMode: "light", GlassTheme: "fog"})
	if !th.Light || th.Background != hexColor("#E5EAF1") || th.Foreground != hexColor("#1C2433") {
		t.Errorf("fog light = %+v", th)
	}
	if th.ui().PanelInputBg != lightUI.PanelInputBg {
		t.Error("light glass keeps white input wells")
	}
}
