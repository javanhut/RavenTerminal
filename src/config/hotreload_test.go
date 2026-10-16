package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestNewKeyDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.ScrollbackLines != 10000 || c.Bell != "visual" || !c.Notifications || c.NotifyCommandAfter != 10 {
		t.Fatalf("defaults: scrollback=%d bell=%q notifications=%v after=%d", c.ScrollbackLines, c.Bell, c.Notifications, c.NotifyCommandAfter)
	}
	if !c.Appearance.Ligatures || !c.Appearance.MarksGutter || c.Appearance.ReducedMotion || c.Appearance.BackgroundOpacity != 1 {
		t.Fatalf("appearance defaults: %+v", c.Appearance)
	}
	// A config written before these keys existed keeps the defaults.
	old := DefaultConfig()
	if _, err := toml.Decode("theme = \"ghostty\"\n[appearance]\ncursor_blink = false\n", old); err != nil {
		t.Fatal(err)
	}
	if old.ScrollbackLines != 10000 || old.Appearance.BackgroundOpacity != 1 || !old.Notifications {
		t.Fatalf("missing keys lost defaults: %+v", old)
	}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(c); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"scrollback_lines", "bell", "notifications", "notify_command_after", "ligatures", "marks_gutter", "reduced_motion", "background_opacity"} {
		if !strings.Contains(sb.String(), key+" =") {
			t.Errorf("saved config lacks %s", key)
		}
	}
}

func TestBellModeAndOpacityNormalize(t *testing.T) {
	c := DefaultConfig()
	c.Bell = "Bogus"
	c.Appearance.BackgroundOpacity = 0
	c.ScrollbackLines = -5
	if c.BellMode() != "visual" || c.Opacity() != 1 || c.Scrollback() != 0 {
		t.Fatalf("normalized: %q %v %d", c.BellMode(), c.Opacity(), c.Scrollback())
	}
	c.Bell, c.Appearance.BackgroundOpacity = "NONE", 0.05
	if c.BellMode() != "none" || c.Opacity() != 0.1 {
		t.Fatalf("normalized: %q %v", c.BellMode(), c.Opacity())
	}
}

func TestWatchStatBumpsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gen atomic.Uint64
	stop := make(chan struct{})
	defer close(stop)
	go watchStat(path, 10*time.Millisecond, &gen, stop)
	time.Sleep(30 * time.Millisecond)
	if gen.Load() != 0 {
		t.Fatal("unchanged file bumped the generation")
	}
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for gen.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("rewrite never bumped the generation")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
