package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestTextRenderingOptions(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Appearance.Ligatures {
		t.Errorf("ligatures must default on")
	}
	if cfg.Appearance.TextGamma <= 0 || cfg.Appearance.TextContrast <= 0 {
		t.Errorf("text gamma/contrast defaults unset: %v %v", cfg.Appearance.TextGamma, cfg.Appearance.TextContrast)
	}
	if _, err := toml.Decode("[appearance]\nligatures = false\ntext_gamma = 1.2\ntext_contrast = 1.3\n", cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.Ligatures || cfg.Appearance.TextGamma != 1.2 || cfg.Appearance.TextContrast != 1.3 {
		t.Errorf("appearance text options not decoded: %+v", cfg.Appearance)
	}
}
