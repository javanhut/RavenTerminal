package main

import (
	"os"
	"strings"
	"testing"
)

func TestScrollbackANSI(t *testing.T) {
	term := termWith(20, 3, "\x1b[31mred\x1b[0m plain\r\n\x1b[1;44mbold\x1b[0m\r\n\x1b[38;2;1;2;3mrgb\x1b[0m\r\n\x1b[38;5;200mix\x1b[0m\r\nlast")
	out := scrollbackANSI(term.GetGrid())
	for _, want := range []string{"\x1b[31mred\x1b[0m plain\n", "\x1b[1;44mbold", "\x1b[38;2;1;2;3mrgb", "\x1b[38;5;200mix", "last"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pager dump lacks %q:\n%q", want, out)
		}
	}
	if strings.HasSuffix(out, "\n\n") || strings.Count(out, "\n") != 5 {
		t.Fatalf("expected 5 lines, no trailing blanks: %q", out)
	}
}

func TestScrollbackPagerFileAndCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	path, err := writeScrollbackFile("hello\n")
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("scrollback file %v perm %v", err, st.Mode().Perm())
	}
	cmd := pagerCommand(path)
	if cmd[0] != "sh" || cmd[len(cmd)-1] != path || !strings.Contains(cmd[2], "less -R +G") || !strings.Contains(cmd[2], "rm -f") {
		t.Fatalf("pager command = %q", cmd)
	}
}
