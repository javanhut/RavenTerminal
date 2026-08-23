package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/tab"
)

// session is the persisted window state: the tab strip's split layouts plus
// which tab was in front. Terminal contents and running processes are not
// saved — restore reopens fresh shells in the same directories.
type session struct {
	Tabs        []tab.Layout `json:"tabs"`
	ActiveIndex int          `json:"active_index"`
}

func sessionPath() string {
	return filepath.Join(config.GetConfigDir(), "session.json")
}

// loadSession reads the previous session. A missing, unreadable, or corrupt
// file just means "no session to restore" — never an error the user has to
// deal with, since the fallback (one fresh tab) is always fine.
func loadSession() session {
	data, err := os.ReadFile(sessionPath())
	if err != nil {
		return session{}
	}
	var s session
	if err := json.Unmarshal(data, &s); err != nil {
		return session{}
	}
	return s
}

// saveSession persists the current tab layout, best-effort. Directories that
// no longer exist are dropped at restore time, not here, so a temporarily
// unmounted path still comes back if it returns.
//
// ponytail: written once, on clean exit. A kill -9 loses the session; move
// this to the tab add/remove paths if that turns out to matter.
func (a *App) saveSession() {
	layouts := a.tabManager.Layouts()
	if len(layouts) == 0 {
		return
	}
	data, err := json.MarshalIndent(session{
		Tabs:        layouts,
		ActiveIndex: a.tabManager.ActiveIndex(),
	}, "", "  ")
	if err != nil {
		return
	}
	_ = config.WriteFileAtomic(sessionPath(), data)
}

// restoredTabManager builds the tab manager, reopening the previous session
// when restore_session is on. Recorded directories that have since disappeared
// are cleared so the pane falls back to the default start directory rather
// than failing to spawn.
func restoredTabManager(cols, rows uint16, restore bool) (*tab.TabManager, error) {
	if !restore {
		return tab.NewTabManager(cols, rows)
	}
	s := loadSession()
	if len(s.Tabs) == 0 {
		return tab.NewTabManager(cols, rows)
	}
	for i := range s.Tabs {
		pruneMissingDirs(&s.Tabs[i])
	}
	return tab.NewTabManagerFromLayouts(cols, rows, s.Tabs, s.ActiveIndex)
}

// pruneMissingDirs blanks any recorded directory that is no longer a readable
// directory, in place.
func pruneMissingDirs(l *tab.Layout) {
	if l.Dir != "" {
		if fi, err := os.Stat(l.Dir); err != nil || !fi.IsDir() {
			l.Dir = ""
		}
	}
	for i := range l.Children {
		pruneMissingDirs(&l.Children[i])
	}
}
