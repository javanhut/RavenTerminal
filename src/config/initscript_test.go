package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeScripts generates init.sh and init.fish into a temp config dir and
// returns both paths.
func writeScripts(t *testing.T, cfg *Config) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	initPath, err := cfg.WriteInitScript()
	if err != nil {
		t.Fatalf("WriteInitScript: %v", err)
	}
	fishPath := FishInitPath()
	if _, err := os.Stat(fishPath); err != nil {
		t.Fatalf("fish init script not written: %v", err)
	}
	return initPath, fishPath
}

func shellPath(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available", name)
	}
	return p
}

// The generated bash script must parse with bash.
func TestInitScriptBashSyntax(t *testing.T) {
	initPath, _ := writeScripts(t, DefaultConfig())
	bash := shellPath(t, "bash")
	out, err := exec.Command(bash, "-n", initPath).CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n failed: %v\n%s", err, out)
	}
}

// The generated fish script must parse AND its functions must run without
// errors — this is the regression test for the "Unexpected ')'" error fish
// printed when it was fed the bash script.
func TestInitScriptFishSyntax(t *testing.T) {
	for _, style := range []string{"full", "simple", "minimal", "custom"} {
		t.Run(style, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Prompt.Style = style
			cfg.Exports = map[string]string{"RAVEN_TEST": `a "quoted" value`}
			cfg.Shell.Paths = []string{"/tmp/raven test bin"}
			_, fishPath := writeScripts(t, cfg)

			fish := shellPath(t, "fish")
			cmd := exec.Command(fish, "--no-config", "-c",
				"source "+FishQuote(fishPath)+"; __raven_detect_lang; __raven_detect_vcs; fish_prompt; __raven_emit_osc7")
			cmd.Dir = t.TempDir()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fish failed: %v\n%s", err, out)
			}
		})
	}
}

// In a git repository, the fish VCS detection must report Git(branch ...)
// with the same counts the bash version computes.
func TestFishVCSDetectMatchesBash(t *testing.T) {
	cfg := DefaultConfig()
	initPath, fishPath := writeScripts(t, cfg)
	fish := shellPath(t, "fish")
	bash := shellPath(t, "bash")
	git := shellPath(t, "git")

	// Build a scratch repo: one committed file, one staged, one modified,
	// one untracked.
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	write("committed.txt", "a")
	run("add", "committed.txt")
	run("commit", "-m", "init")
	write("staged.txt", "b")
	run("add", "staged.txt")
	write("committed.txt", "changed")
	write("untracked.txt", "c")

	fishCmd := exec.Command(fish, "--no-config", "-c", "source "+FishQuote(fishPath)+"; __raven_detect_vcs")
	fishCmd.Dir = repo
	fishOut, err := fishCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish vcs detect: %v\n%s", err, fishOut)
	}

	bashCmd := exec.Command(bash, "-c", ". "+shQuote(initPath)+"; __raven_detect_vcs")
	bashCmd.Dir = repo
	bashOut, err := bashCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash vcs detect: %v\n%s", err, bashOut)
	}

	fishVCS := strings.TrimSpace(string(fishOut))
	bashVCS := strings.TrimSpace(string(bashOut))
	if fishVCS != bashVCS {
		t.Errorf("fish %q != bash %q", fishVCS, bashVCS)
	}
	if !strings.Contains(fishVCS, "Git(main") {
		t.Errorf("vcs output %q does not contain Git(main", fishVCS)
	}
	for _, marker := range []string{"+1", "~1", "?1"} {
		if !strings.Contains(fishVCS, marker) {
			t.Errorf("vcs output %q missing %s (staged/unstaged/untracked)", fishVCS, marker)
		}
	}
}

// Customized (bash-syntax) detect scripts must still work from fish via the
// bash -c bridge, including bodies that use `return`.
func TestFishCustomDetectScriptBridge(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scripts.LanguageDetect = `[ -f marker.txt ] && echo "Marked" && return 0
echo "Unmarked"`
	_, fishPath := writeScripts(t, cfg)
	fish := shellPath(t, "fish")
	shellPath(t, "bash")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(fish, "--no-config", "-c", "source "+FishQuote(fishPath)+"; __raven_detect_lang")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "Marked" {
		t.Errorf("custom detect via bash bridge = %q, want Marked", got)
	}
}

// shQuote single-quotes s for POSIX shells (test helper).
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// A bash-syntax user init script must not take down the rest of init.fish:
// fish aborts a whole file on parse errors, so the user init is sub-sourced
// from its own file.
func TestFishBashUserInitIsContained(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scripts.Init = `if [ -d "$HOME/bin" ]; then PATH="$HOME/bin:$PATH"; fi`
	_, fishPath := writeScripts(t, cfg)
	fish := shellPath(t, "fish")

	// The contained parse error is still reported on stderr; what matters is
	// that the functions defined after the user init survive (stdout).
	cmd := exec.Command(fish, "--no-config", "-c",
		"source "+FishQuote(fishPath)+"; functions -q __raven_detect_lang; and __raven_detect_lang")
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("raven functions lost after bash user init: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "None" {
		t.Errorf("__raven_detect_lang = %q, want None", got)
	}
}

// Bash-style ${NAME} in export and PATH values is a hard parse error in fish
// (aborting the whole script); it must be translated to $NAME so the value
// expands the same way it does in bash.
func TestFishExportBracedVarExpansion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exports = map[string]string{
		"RAVEN_BRACED":  `${HOME}/go`,
		"RAVEN_COMPLEX": `${RAVEN_UNSET:-x}`, // complex expansion stays literal but must not abort
	}
	cfg.Shell.Paths = []string{`${HOME}/raven-bin`}
	_, fishPath := writeScripts(t, cfg)
	fish := shellPath(t, "fish")

	cmd := exec.Command(fish, "--no-config", "-c",
		"source "+FishQuote(fishPath)+"; echo $RAVEN_BRACED; contains -- \"$HOME/raven-bin\" $PATH; and echo PATH_OK")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish failed: %v\n%s", err, out)
	}
	home := os.Getenv("HOME")
	text := string(out)
	if !strings.Contains(text, home+"/go") {
		t.Errorf("RAVEN_BRACED did not expand ${HOME}: %q", text)
	}
	if !strings.Contains(text, "PATH_OK") {
		t.Errorf("PATH addition with ${HOME} did not expand: %q", text)
	}
}

// With a custom (bash) prompt script, fish cannot run it; it must fall back
// to the full informative prompt, not a bare "> ".
func TestFishCustomPromptFallsBackToFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Prompt.Style = "custom"
	cfg.Prompt.CustomPromptScript = `PS1="custom> "`
	_, fishPath := writeScripts(t, cfg)
	fish := shellPath(t, "fish")

	cmd := exec.Command(fish, "--no-config", "-c", "source "+FishQuote(fishPath)+"; fish_prompt")
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Lang:") {
		t.Errorf("custom style did not fall back to full prompt: %q", out)
	}
}

// The generated rsh script must parse and run with ravenshell: define the
// detect/prompt functions, then produce a prompt containing the Lang segment
// (full style) without errors.
func TestInitScriptRshSyntax(t *testing.T) {
	for _, style := range []string{"full", "simple", "minimal", "custom"} {
		t.Run(style, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Prompt.Style = style
			cfg.Exports = map[string]string{"RAVEN_TEST": "plain value"}
			writeScripts(t, cfg)
			rshPath := RavenInitPath()
			content, err := os.ReadFile(rshPath)
			if err != nil {
				t.Fatalf("rsh init script not written: %v", err)
			}

			ravenshell := shellPath(t, "ravenshell")

			// Capability probe: the generated script needs zero-parameter
			// function syntax. An installed ravenshell that predates it cannot
			// judge the generator — skip rather than fail on a stale binary.
			probe := filepath.Join(t.TempDir(), "probe.rsh")
			if err := os.WriteFile(probe, []byte("func __probe() {\n  return \"ok\"\n}\nprint __probe()\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(ravenshell, probe).CombinedOutput(); err != nil {
				t.Skipf("installed ravenshell predates zero-arg function syntax; update it to run this test: %v\n%s", err, out)
			}

			// The prompt function takes a status parameter only in the full
			// style (custom without a script degrades to minimal).
			call := "prompt()"
			if style == "full" {
				call = "prompt(0)"
			}
			driver := string(content) + "\nprint __raven_detect_lang()\nprint __raven_detect_vcs()\nprint " + call + "\n"
			driverPath := filepath.Join(t.TempDir(), "driver.rsh")
			if err := os.WriteFile(driverPath, []byte(driver), 0644); err != nil {
				t.Fatal(err)
			}

			workDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module x\n"), 0644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(ravenshell, driverPath)
			cmd.Dir = workDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("ravenshell failed: %v\n%s", err, out)
			}
			if strings.Contains(string(out), "parse error") || strings.Contains(string(out), "error:") {
				t.Fatalf("ravenshell reported errors:\n%s", out)
			}
			if !strings.Contains(string(out), "Go") {
				t.Errorf("language detection did not report Go:\n%s", out)
			}
			if style == "full" && !strings.Contains(string(out), "Lang:") {
				t.Errorf("full prompt missing Lang segment:\n%s", out)
			}
		})
	}
}
