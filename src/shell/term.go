package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// RavenTerm is the terminal's own terminfo entry (terminfo/xterm-raven.ti).
	RavenTerm = "xterm-raven"
	// FallbackTerm is used when xterm-raven is not installed.
	FallbackTerm = "xterm-256color"
)

// TermName returns the TERM value for child shells: xterm-raven when its
// terminfo entry is resolvable, xterm-256color otherwise.
func TermName() string {
	if terminfoAvailable(RavenTerm, terminfoDirs()) {
		return RavenTerm
	}
	return FallbackTerm
}

// terminfoDirs lists the directories ncurses searches, in its order.
func terminfoDirs() []string {
	var dirs []string
	if d := os.Getenv("TERMINFO"); d != "" {
		dirs = append(dirs, d)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".terminfo"))
	}
	for d := range strings.SplitSeq(os.Getenv("TERMINFO_DIRS"), ":") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return append(dirs, "/usr/share/terminfo", "/usr/lib/terminfo", "/etc/terminfo", "/usr/local/share/terminfo")
}

// terminfoAvailable reports whether name has a compiled entry in one of dirs,
// under either the letter (x/) or the hex (78/) directory layout.
func terminfoAvailable(name string, dirs []string) bool {
	if name == "" {
		return false
	}
	letter := name[:1]
	hex := fmt.Sprintf("%02x", name[0])
	for _, d := range dirs {
		if d == "" {
			continue
		}
		for _, sub := range []string{letter, hex} {
			if st, err := os.Stat(filepath.Join(d, sub, name)); err == nil && st.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}
