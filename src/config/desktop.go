package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
)

// The slice of RavenSettingsUI's desktop.toml the "raven" theme follows:
// [appearance] theme_mode and accent. Settings owns the file and writes it
// atomically (rename over); every key is optional and a parse error means
// the defaults, so a newer Settings never breaks an older terminal.

// DefaultDesktopAccent is the accent used when desktop.toml has no valid one.
const DefaultDesktopAccent = "#7AA2F7"

// DesktopAppearance is the [appearance] section of desktop.toml.
type DesktopAppearance struct {
	ThemeMode string `toml:"theme_mode"` // "light" | "dark" | "auto"
	Accent    string `toml:"accent"`     // "#RRGGBB"
}

type desktopFile struct {
	Appearance DesktopAppearance `toml:"appearance"`
}

// DesktopConfigPath returns $XDG_CONFIG_HOME/raven/desktop.toml, falling
// back to ~/.config/raven/desktop.toml.
func DesktopConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".config")
		} else {
			base = ".config"
		}
	}
	return filepath.Join(base, "raven", "desktop.toml")
}

// LoadDesktopAppearance reads desktop.toml. A missing or unparseable file
// yields the defaults (dark, default accent).
func LoadDesktopAppearance() DesktopAppearance {
	data, err := os.ReadFile(DesktopConfigPath())
	if err != nil {
		return DesktopAppearance{}
	}
	return ParseDesktopAppearance(string(data))
}

// ParseDesktopAppearance parses desktop.toml content; errors mean defaults.
func ParseDesktopAppearance(data string) DesktopAppearance {
	var f desktopFile
	if _, err := toml.Decode(data, &f); err != nil {
		return DesktopAppearance{}
	}
	return f.Appearance
}

// IsLight reports whether the desktop asks for a light palette. "auto" is
// dark everywhere in Raven apps, as is anything unrecognised.
func (d DesktopAppearance) IsLight() bool {
	return d.ThemeMode == "light"
}

// AccentHex returns the accent as #RRGGBB, or the default when it is not one.
func (d DesktopAppearance) AccentHex() string {
	if isHexColor(d.Accent) {
		return d.Accent
	}
	return DefaultDesktopAccent
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	return strings.Trim(s[1:], "0123456789abcdefABCDEF") == ""
}

var (
	desktopWatchOnce sync.Once
	desktopGen       atomic.Uint64
)

// DesktopGeneration returns a counter that bumps whenever desktop.toml
// changes (created, rewritten, renamed over, or removed). The first call
// starts a background poller shared by every window; callers compare the
// value against the last one they applied, on their own (main) thread.
func DesktopGeneration() uint64 {
	desktopWatchOnce.Do(func() { go watchDesktop(DesktopConfigPath(), time.Second) })
	return desktopGen.Load()
}

// watchDesktop polls the file's stat once per interval. Polling (rather than
// inotify) keeps this dependency-free and portable, and survives the file
// not existing yet or being replaced by rename; the interval doubles as the
// debounce for Settings' burst of writes.
func watchDesktop(path string, interval time.Duration) {
	prev, prevErr := os.Stat(path)
	for {
		time.Sleep(interval)
		cur, err := os.Stat(path)
		if desktopStatChanged(prev, prevErr, cur, err) {
			desktopGen.Add(1)
		}
		prev, prevErr = cur, err
	}
}

func desktopStatChanged(prev os.FileInfo, prevErr error, cur os.FileInfo, curErr error) bool {
	if (prevErr == nil) != (curErr == nil) {
		return true
	}
	if curErr != nil {
		return false
	}
	return !cur.ModTime().Equal(prev.ModTime()) || cur.Size() != prev.Size() || !os.SameFile(prev, cur)
}
