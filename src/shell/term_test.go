package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminfoAvailable(t *testing.T) {
	plain := t.TempDir()
	letter := t.TempDir()
	hex := t.TempDir()
	for dir, sub := range map[string]string{letter: "x", hex: "78"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "xterm-raven"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if terminfoAvailable("xterm-raven", []string{plain, ""}) {
		t.Error("found entry in empty dirs")
	}
	if !terminfoAvailable("xterm-raven", []string{plain, letter}) {
		t.Error("x/xterm-raven not found")
	}
	if !terminfoAvailable("xterm-raven", []string{hex}) {
		t.Error("78/xterm-raven not found")
	}
	if terminfoAvailable("", []string{letter}) {
		t.Error("empty name matched")
	}
}

func TestTermNameFollowsTerminfo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TERMINFO_DIRS", "")
	empty := t.TempDir()
	t.Setenv("TERMINFO", empty)
	dirs := terminfoDirs()
	if dirs[0] != empty || dirs[1] != filepath.Join(home, ".terminfo") {
		t.Fatalf("search order = %v", dirs)
	}
	for _, d := range []string{"/usr/share/terminfo", "/usr/lib/terminfo", "/etc/terminfo"} {
		found := false
		for _, x := range dirs {
			found = found || x == d
		}
		if !found {
			t.Errorf("%s not searched: %v", d, dirs)
		}
	}
	if !terminfoAvailable(RavenTerm, dirs) && TermName() != FallbackTerm {
		t.Errorf("TermName() = %q without an xterm-raven entry", TermName())
	}
	if err := os.MkdirAll(filepath.Join(home, ".terminfo", "x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".terminfo", "x", "xterm-raven"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := TermName(); got != RavenTerm {
		t.Errorf("TermName() = %q with ~/.terminfo entry, want %q", got, RavenTerm)
	}
}

func TestRavenTerminfoCompiles(t *testing.T) {
	tic, err := exec.LookPath("tic")
	if err != nil {
		t.Skip("tic not available")
	}
	out := t.TempDir()
	if b, err := exec.Command(tic, "-x", "-o", out, "../../terminfo/xterm-raven.ti").CombinedOutput(); err != nil {
		t.Fatalf("tic: %v\n%s", err, b)
	}
	if !terminfoAvailable(RavenTerm, []string{out}) {
		t.Fatal("compiled entry not found")
	}
	cmd := exec.Command("infocmp", "-x", "-1", RavenTerm)
	cmd.Env = append(os.Environ(), "TERMINFO="+out)
	b, err := cmd.Output()
	if err != nil {
		t.Skipf("infocmp: %v", err)
	}
	caps := string(b)
	for _, want := range []string{
		"\tTc,", "\tRGB,", "\tSu,", "\tfullkbd,", "\tXT,", "\ths,", "\tbce,", "colors#0x100,",
		"Smulx=\\E[4:%p1%dm,", "Setulc=\\E[58:2:", "Sync=\\E[?2026%?%p1%{1}%-%tl%eh%;,",
		"Ms=\\E]52;%p1%s;%p2%s\\007,", "Ss=\\E[%p1%d q,", "Se=\\E[2 q,",
		"tsl=\\E]2;,", "fsl=^G,", "dsl=\\E]2;\\E\\\\,", "kxIN=\\E[I,", "kxOUT=\\E[O,",
	} {
		if !strings.Contains(caps, want) {
			t.Errorf("xterm-raven lacks %q", want)
		}
	}
}
