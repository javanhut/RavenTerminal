package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// zshScripts generates the init scripts into a temp HOME and returns the
// paths of the generated .zshenv and raven-init.zsh.
func zshScripts(t *testing.T, cfg *Config) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if _, err := cfg.WriteInitScript(); err != nil {
		t.Fatalf("WriteInitScript: %v", err)
	}
	dir := filepath.Join(GetScriptsDir(), "zsh")
	return filepath.Join(dir, ".zshenv"), filepath.Join(dir, "raven-init.zsh")
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// The ZDOTDIR shim .zshenv must chain to the user's real ~/.zshenv, restore
// ZDOTDIR so .zprofile/.zshrc load normally, source raven-init.zsh, and guard
// against applying twice.
func TestZshenvShimContents(t *testing.T) {
	zshenvPath, initPath := zshScripts(t, DefaultConfig())
	content := readTestFile(t, zshenvPath)
	for _, want := range []string{
		`"$HOME/.zshenv"`,    // sources the user's real zshenv
		`ZDOTDIR="$HOME"`,    // resets ZDOTDIR to the user's home
		"RAVEN_ZDOTDIR_DONE", // recursion guard
	} {
		if !strings.Contains(content, want) {
			t.Errorf(".zshenv missing %q:\n%s", want, content)
		}
	}
	if !strings.Contains(content, initPath) {
		t.Errorf(".zshenv does not source raven-init.zsh (%s):\n%s", initPath, content)
	}
}

// raven-init.zsh must be native zsh: no bash PROMPT_COMMAND, no bash \[ \]
// zero-width markers; the prompt is driven by a precmd hook using zsh
// %-escapes.
func TestZshInitNoBashisms(t *testing.T) {
	for _, style := range []string{"full", "simple", "minimal", "custom"} {
		t.Run(style, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Prompt.Style = style
			_, initPath := zshScripts(t, cfg)
			content := readTestFile(t, initPath)
			if strings.Contains(content, "PROMPT_COMMAND") {
				t.Errorf("zsh init contains bash PROMPT_COMMAND:\n%s", content)
			}
			if strings.Contains(content, `\[`) {
				t.Errorf(`zsh init contains bash \[ prompt markers:`+"\n%s", content)
			}
			if !strings.Contains(content, "precmd") {
				t.Errorf("zsh init has no precmd hook:\n%s", content)
			}
			// custom without a script degrades to the minimal "> " prompt,
			// which needs no %-escapes (parity with bash).
			if (style == "full" || style == "simple") && !strings.Contains(content, "%~") && !strings.Contains(content, "%F{") {
				t.Errorf("zsh init prompt uses no zsh %%-escapes:\n%s", content)
			}
		})
	}
}

// Exports and aliases from the config must be emitted zsh-compatibly quoted.
func TestZshInitExportsAndAliases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exports = map[string]string{"RAVEN_TEST": `a "quoted" value`}
	cfg.Aliases = map[string]string{"rvnhi": `echo 'hi'`}
	_, initPath := zshScripts(t, cfg)
	content := readTestFile(t, initPath)

	if !strings.Contains(content, `export RAVEN_TEST="a \"quoted\" value"`) {
		t.Errorf("export not emitted with escaped quotes:\n%s", content)
	}
	if !strings.Contains(content, `alias rvnhi='echo '\''hi'\'''`) {
		t.Errorf("alias with single quote not emitted safely:\n%s", content)
	}
}

// Both generated files must parse with zsh.
func TestZshInitScriptSyntax(t *testing.T) {
	zsh := shellPath(t, "zsh")
	for _, style := range []string{"full", "simple", "minimal", "custom"} {
		t.Run(style, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Prompt.Style = style
			cfg.Exports = map[string]string{"RAVEN_TEST": `a "quoted" value`}
			cfg.Aliases = map[string]string{"rvnhi": `echo 'hi'`}
			cfg.Shell.Paths = []string{"/tmp/raven test bin"}
			zshenvPath, initPath := zshScripts(t, cfg)
			for _, f := range []string{zshenvPath, initPath} {
				out, err := exec.Command(zsh, "-n", f).CombinedOutput()
				if err != nil {
					t.Errorf("zsh -n %s failed: %v\n%s", f, err, out)
				}
			}
		})
	}
}

// Sourcing raven-init.zsh into zsh must define working detect functions and a
// precmd hook that sets PS1 (full style: path + Lang segment).
func TestZshInitFunctionalPrompt(t *testing.T) {
	zsh := shellPath(t, "zsh")
	shellPath(t, "bash") // detect bodies run through bash
	cfg := DefaultConfig()
	_, initPath := zshScripts(t, cfg)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(zsh, "--no-rcs", "-c",
		"source "+shQuote(initPath)+"; __raven_detect_lang; __raven_precmd; print -r -- \"$PS1\"")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh failed: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "Go") {
		t.Errorf("language detection did not report Go:\n%s", text)
	}
	if !strings.Contains(text, "Lang:") {
		t.Errorf("full prompt PS1 missing Lang segment:\n%s", text)
	}
	if !strings.Contains(text, "%~") && !strings.Contains(text, "%F{") {
		t.Errorf("PS1 does not use zsh %%-escapes:\n%s", text)
	}
}

// An alias whose value contains single quotes must survive into a live zsh.
func TestZshInitAliasRuns(t *testing.T) {
	zsh := shellPath(t, "zsh")
	cfg := DefaultConfig()
	cfg.Aliases = map[string]string{"rvnhi": `echo 'hi from raven'`}
	_, initPath := zshScripts(t, cfg)

	// eval: zsh expands aliases at parse time, so the alias defined by the
	// source must be evaluated in a later parsing pass.
	cmd := exec.Command(zsh, "--no-rcs", "-i", "-c", "source "+shQuote(initPath)+"; eval rvnhi")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh alias run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hi from raven") {
		t.Errorf("alias did not run: %q", out)
	}
}
