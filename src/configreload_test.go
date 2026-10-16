package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/config"
)

func TestDiffConfig(t *testing.T) {
	base := config.DefaultConfig()
	if d := diffConfig(base, config.DefaultConfig()); d != (configDelta{}) {
		t.Fatalf("identical configs differ: %+v", d)
	}
	if d := diffConfig(nil, base); d != allChanged() {
		t.Fatalf("nil old must apply everything: %+v", d)
	}

	mut := func(f func(c *config.Config)) configDelta {
		c := config.DefaultConfig()
		f(c)
		return diffConfig(base, c)
	}
	cases := []struct {
		name string
		f    func(c *config.Config)
		want configDelta
	}{
		{"theme", func(c *config.Config) { c.Theme = "ghostty" }, configDelta{theme: true}},
		{"opacity", func(c *config.Config) { c.Appearance.BackgroundOpacity = 0.8 }, configDelta{theme: true}},
		{"font", func(c *config.Config) { c.FontSize = 20 }, configDelta{font: true}},
		{"style", func(c *config.Config) { c.Appearance.Undercurl = false }, configDelta{textStyle: true}},
		{"ligatures", func(c *config.Config) { c.Appearance.Ligatures = !c.Appearance.Ligatures }, configDelta{textStyle: true}},
		{"text gamma", func(c *config.Config) { c.Appearance.TextGamma += 0.1 }, configDelta{textStyle: true}},
		{"text contrast", func(c *config.Config) { c.Appearance.TextContrast += 0.1 }, configDelta{textStyle: true}},
		{"scrollback", func(c *config.Config) { c.ScrollbackLines = 500 }, configDelta{scrollback: true}},
		{"panels", func(c *config.Config) { c.Appearance.PanelWidthPercent = 40 }, configDelta{panels: true}},
		{"ollama", func(c *config.Config) { c.Ollama.Model = "qwen" }, configDelta{panels: true}},
		{"clipboard", func(c *config.Config) { c.AllowClipboardRead = true }, configDelta{clipboard: true}},
		{"motion", func(c *config.Config) { c.Appearance.ReducedMotion = true }, configDelta{motion: true}},
		{"bell is read live", func(c *config.Config) { c.Bell = "none" }, configDelta{}},
	}
	for _, c := range cases {
		if got := mut(c.f); got != c.want {
			t.Errorf("%s: delta = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestToU8(t *testing.T) {
	for _, c := range []struct {
		in   float32
		want uint8
	}{{0, 0}, {1, 255}, {0.5, 128}, {-1, 0}, {2, 255}} {
		if got := to8(c.in); got != c.want {
			t.Errorf("to8(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
