package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopGlassColorsAllThemes(t *testing.T) {
	cases := []struct {
		names            []string
		dbg, dfg, lb, lf string
	}{
		{[]string{"tokyo-neon", "Tokyo Neon", "TokyoNeonGlass", "Tokyo Neon Glass", "tokyoneon"}, "#0A1A14", "#E6FFF1", "#E2F6EA", "#06291A"},
		{[]string{"clear", "Clear Glass", "clear-glass"}, "#1C1F26", "#FFFFFF", "#F7F9FC", "#0F1218"},
		{[]string{"ember", "Ember Glass", "EmberGlass"}, "#3A2418", "#FFF3E8", "#F8EBDF", "#3A1F10"},
		{[]string{"nebula", "Nebula Glass", "nebula-glass"}, "#24163A", "#F3EAFF", "#EEE6FA", "#22123C"},
		{[]string{"fog", "Fog Glass"}, "#6E7D94", "#FFFFFF", "#E5EAF1", "#1C2433"},
		{[]string{"midnight", "MidnightGlass"}, "#0E1630", "#E8EEFF", "#E3E7F6", "#101A3A"},
	}
	for _, c := range cases {
		for _, n := range c.names {
			g, fg, ok := (DesktopAppearance{GlassTheme: n}).GlassColors()
			if !ok || g != c.dbg || fg != c.dfg {
				t.Errorf("%q dark: %s %s %v", n, g, fg, ok)
			}
			g, fg, ok = (DesktopAppearance{ThemeMode: "light", GlassTheme: n}).GlassColors()
			if !ok || g != c.lb || fg != c.lf {
				t.Errorf("%q light: %s %s %v", n, g, fg, ok)
			}
		}
	}
}

func runBashInteractive(t *testing.T, initPath, input string, env ...string) string {
	t.Helper()
	bash := shellPath(t, "bash")
	cmd := exec.Command(bash, "--noprofile", "--rcfile", initPath, "-i")
	cmd.Stdin = strings.NewReader(input)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), env...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestBashOSC133(t *testing.T) {
	for _, style := range []string{"full", "simple", "minimal", "custom"} {
		t.Run(style, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Shell.SourceRC = false
			cfg.Prompt.Style = style
			initPath, _ := writeScripts(t, cfg)
			out := runBashInteractive(t, initPath, "(exit 3)\n\ntrue\nexit\n")
			for _, want := range []string{
				"\x1b]133;C\a\x1b]133;D;3\a",
				"\x1b]133;C\a\x1b]133;D;0\a",
				"\x1b]133;B\a",
				"\x1b]7;file://",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in %q", want, out)
				}
			}
			// The empty line ran nothing: no C, so no D for it, and the
			// first prompt has no D either.
			if n := strings.Count(out, "\x1b]133;D;"); n != 2 {
				t.Errorf("D count = %d, want 2\n%q", n, out)
			}
			if n := strings.Count(out, "\x1b]133;A\a"); n != 4 {
				t.Errorf("A count = %d, want 4\n%q", n, out)
			}
			if i := strings.Index(out, "\x1b]133;D;3"); i < 0 || !strings.Contains(out[i:], "\x1b]133;A\a") {
				t.Errorf("no prompt start after D: %q", out)
			}
			if strings.Index(out, "\x1b]133;D") < strings.Index(out, "\x1b]133;A") {
				t.Errorf("first prompt emitted D before any command: %q", out)
			}
		})
	}
}

func TestBashOSC133NoDuplicateOnResource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Shell.SourceRC = false
	initPath, _ := writeScripts(t, cfg)
	out := runBashInteractive(t, initPath, "source "+shQuote(initPath)+"\ntrue\nexit\n")
	if strings.Contains(out, "\x1b]133;C\a\x1b]133;C\a") || strings.Contains(out, "\x1b]133;A\a\x1b]133;A\a") {
		t.Errorf("marks doubled after re-source: %q", out)
	}
}

func TestBashRavenFunctions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Shell.SourceRC = false
	initPath, _ := writeScripts(t, cfg)
	bin := t.TempDir()
	stub := "#!/bin/sh\necho \"stub:$*\"\n"
	if err := os.WriteFile(filepath.Join(bin, "raven-terminal"), []byte(stub), 0755); err != nil {
		t.Fatal(err)
	}
	bash := shellPath(t, "bash")
	cmd := exec.Command(bash, "--noprofile", "--norc", "-c",
		"source "+shQuote(initPath)+"; PATH="+shQuote(bin)+":$PATH; list-fonts; keybindings; change-font JetBrains Mono; change-font")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{
		"stub:--list-fonts\n",
		"stub:--keybindings\n",
		"\x1b]7771;change-font=JetBrains Mono\x1b\\",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	if strings.Count(s, "stub:--list-fonts") != 2 {
		t.Errorf("change-font without args should list fonts: %q", s)
	}
}

func TestZshInitIntegration(t *testing.T) {
	_, initPath := zshScripts(t, DefaultConfig())
	s := readTestFile(t, initPath)
	for _, want := range []string{
		`\033]133;C\a`,
		`\033]133;D;%s\a`,
		`%{\e]133;A\a%}`,
		`%{\e]133;B\a%}`,
		"preexec_functions",
		"list-fonts() {",
		"raven-terminal --list-fonts",
		"keybindings() {",
		"raven-terminal --keybindings",
		"change-font() {",
		`\033]7771;change-font=%s\033\\`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh init missing %q", want)
		}
	}
}

func TestFishInitIntegration(t *testing.T) {
	writeScripts(t, DefaultConfig())
	s := readTestFile(t, FishInitPath())
	for _, want := range []string{
		"--on-event fish_preexec",
		"--on-event fish_postexec",
		`\033]133;C\a`,
		`\033]133;D;%s\a`,
		`\033]133;A\a`,
		`\033]133;B\a`,
		"function list-fonts",
		"raven-terminal --list-fonts",
		"function keybindings",
		"raven-terminal --keybindings",
		"function change-font",
		`\033]7771;change-font=%s\033\\`,
		"FISH_VERSION",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("fish init missing %q", want)
		}
	}
}

func TestRavenInitIntegration(t *testing.T) {
	writeScripts(t, DefaultConfig())
	s := readTestFile(t, RavenInitPath())
	for _, want := range []string{
		"raven-alias list-fonts raven-terminal --list-fonts",
		"raven-alias keybindings raven-terminal --keybindings",
		"raven-alias change-font sh -c",
		"7771;change-font=",
		"RAVENSHELL_OSC133",
		"\x1b]133;A\a",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rsh init missing %q\n%s", want, s)
		}
	}
}

// The rsh fallback marks only apply when the running ravenshell does not
// emit them itself; run the prompt under both settings.
func TestRavenInitPromptMarksFollowNativeSupport(t *testing.T) {
	ravenshell := shellPath(t, "ravenshell")
	writeScripts(t, DefaultConfig())
	content := readTestFile(t, RavenInitPath())
	driver := filepath.Join(t.TempDir(), "d.rsh")
	if err := os.WriteFile(driver, []byte(content+"\nprint prompt(0)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(native string) string {
		cmd := exec.Command(ravenshell, driver)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "RAVENSHELL_OSC133="+native)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Skipf("ravenshell cannot run the init script: %v\n%s", err, out)
		}
		return string(out)
	}
	if out := run(""); !strings.Contains(out, "\x1b]133;A\a") {
		t.Errorf("fallback prompt lacks A mark: %q", out)
	}
	if out := run("1"); strings.Contains(out, "\x1b]133;") {
		t.Errorf("native ravenshell prompt must not carry marks: %q", out)
	}
}

func TestWriteInitScriptAtomicAndUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	path, err := cfg.WriteInitScript()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{path, FishInitPath(), RavenInitPath(), ZshInitPath(), filepath.Join(ZshDotDir(), ".zshenv")}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, p := range paths {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cfg.WriteInitScript(); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !st.ModTime().Equal(old) {
			t.Errorf("%s rewritten although unchanged", p)
		}
	}
	entries, _ := os.ReadDir(GetScriptsDir())
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}

	cfg.Exports = map[string]string{"RAVEN_CHANGED": "1"}
	if _, err := cfg.WriteInitScript(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readTestFile(t, path), "RAVEN_CHANGED") {
		t.Error("changed config not written")
	}
}

func TestWriteIfChanged(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := writeIfChanged(p, []byte("a"), 0755); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0755 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	if err := writeIfChanged(p, []byte("b"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, p); got != "b" {
		t.Errorf("content = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("stray files: %v", entries)
	}
}

func TestRavenInitCommandsRun(t *testing.T) {
	ravenshell := shellPath(t, "ravenshell")
	writeScripts(t, DefaultConfig())
	content := readTestFile(t, RavenInitPath())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "raven-terminal"), []byte("#!/bin/sh\necho \"stub:$*\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(t.TempDir(), "d.rsh")
	body := content + "\nlist-fonts\nkeybindings\nchange-font JetBrains Mono\nchange-font\n"
	if err := os.WriteFile(driver, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ravenshell, driver)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"stub:--list-fonts\n", "stub:--keybindings\n", "\x1b]7771;change-font=JetBrains Mono\x1b\\"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	if strings.Count(s, "stub:--list-fonts") != 2 {
		t.Errorf("change-font without args should list fonts: %q", s)
	}
}
