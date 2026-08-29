package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Fish-syntax init script generation. The bash init.sh cannot be sourced by
// fish (process substitution, ${var:0:2}, case/esac, PS1/PROMPT_COMMAND, ...),
// so fish gets its own translation, written alongside init.sh and loaded via
// `fish -C "source <path>"` (see shell/pty.go) or re-sourced into a running
// fish by the settings menu (see main.go OnInitScriptUpdated).

// FishInitPath returns the path of the generated fish init script.
func FishInitPath() string {
	return filepath.Join(GetScriptsDir(), "init.fish")
}

// FishQuote quotes s as a fish single-quoted string (only backslash and the
// single quote are special inside fish single quotes).
func FishQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

var bashBracedVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// fishDoubleQuote escapes a value for use inside fish double quotes while
// keeping $VAR expansion working (parity with the bash export emission).
// Bash-style ${NAME} is translated to $NAME; any remaining "${" (complex
// bash parameter expansion) is a hard parse error in fish that would abort
// the whole script, so its $ is escaped to stay literal instead.
func fishDoubleQuote(s string) string {
	s = escapeDoubleQuotes(s)
	s = bashBracedVar.ReplaceAllString(s, "$$$1")
	s = strings.ReplaceAll(s, "${", `\${`)
	return s
}

// writeFishInitScript writes the fish variant of the init script and returns
// its path.
func (c *Config) writeFishInitScript() (string, error) {
	path := FishInitPath()
	var b strings.Builder

	b.WriteString("# Raven Terminal Init Script (fish) - Auto-generated\n")
	b.WriteString("# Do not edit directly - changes will be overwritten\n")
	b.WriteString("# Edit config.toml instead\n\n")

	// Fish reads its own config.fish when launched without --no-config, so
	// there is no bashrc-sourcing section here.

	// User init script. The shared [scripts] init config value must be fish
	// syntax for fish shells. It goes into its own sub-sourced file: fish
	// aborts a whole file on any parse error, so inlining a bash-syntax init
	// here would take down the prompt, detect functions, aliases, and exports
	// with it. A parse error in a sub-sourced file only aborts that file.
	if c.Scripts.Init != "" {
		userPath := filepath.Join(GetScriptsDir(), "user-init.fish")
		if err := os.WriteFile(userPath, []byte(c.Scripts.Init+"\n"), 0644); err == nil {
			b.WriteString("# User init script (must be valid fish syntax; kept in its own\n")
			b.WriteString("# file so a syntax error cannot break the rest of this script)\n")
			b.WriteString("source " + FishQuote(userPath) + "\n\n")
		}
	}

	b.WriteString("# Language detection function\n")
	b.WriteString(c.fishDetectFunction("__raven_detect_lang", c.Scripts.LanguageDetect, defaultLanguageDetect, fishDefaultLanguageDetect))
	b.WriteString("\n")

	b.WriteString("# VCS detection function\n")
	b.WriteString(c.fishDetectFunction("__raven_detect_vcs", c.Scripts.VCSDetect, defaultVCSDetect, fishDefaultVCSDetect))
	b.WriteString("\n")

	b.WriteString("# Emit OSC 7 for current working directory\n")
	b.WriteString("function __raven_emit_osc7\n")
	b.WriteString("    set -l _host $hostname\n")
	b.WriteString("    test -z \"$_host\"; and set _host (hostname 2>/dev/null)\n")
	b.WriteString("    printf '\\033]7;file://%s%s\\a' \"$_host\" \"$PWD\"\n")
	b.WriteString("end\n\n")

	b.WriteString(c.buildFishPromptFunction())

	// Aliases (sorted for deterministic output).
	if len(c.Aliases) > 0 {
		b.WriteString("\n# Aliases\n")
		names := make([]string, 0, len(c.Aliases))
		for name := range c.Aliases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b.WriteString("alias " + name + " " + FishQuote(c.Aliases[name]) + "\n")
		}
	}

	// Exports.
	if len(c.Exports) > 0 {
		b.WriteString("\n# Exports\n")
		names := make([]string, 0, len(c.Exports))
		for name := range c.Exports {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b.WriteString("set -gx " + name + " \"" + fishDoubleQuote(c.Exports[name]) + "\"\n")
		}
	}

	// PATH additions (idempotent, like the case-guard in init.sh).
	if len(c.Shell.Paths) > 0 {
		b.WriteString("\n# Raven PATH additions\n")
		for _, dir := range c.Shell.Paths {
			d := fishDoubleQuote(dir)
			b.WriteString("contains -- \"" + d + "\" $PATH; or set -gx PATH \"" + d + "\" $PATH\n")
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// fishDetectFunction emits a fish function for a detect script. The default
// bash bodies have hand-written fish translations; a user-customized body is
// bash syntax the settings menu documents, so it runs through `bash -c`
// (wrapped in a function so `return` statements work).
func (c *Config) fishDetectFunction(name, body, bashDefault, fishDefault string) string {
	var b strings.Builder
	b.WriteString("function " + name + "\n")
	switch strings.TrimSpace(body) {
	case "":
		b.WriteString("    echo 'None'\n")
	case strings.TrimSpace(bashDefault):
		b.WriteString(fishDefault)
	default:
		wrapped := "__raven_fn() {\n" + body + "\n}; __raven_fn"
		b.WriteString("    if command -q bash\n")
		b.WriteString("        bash -c " + FishQuote(wrapped) + "\n")
		b.WriteString("    else\n")
		b.WriteString("        echo 'None'\n")
		b.WriteString("    end\n")
	}
	b.WriteString("end\n")
	return b.String()
}

// Fish translation of the default language-detect body (see
// defaultLanguageDetect in config.go).
const fishDefaultLanguageDetect = `    # Detect project language
    test -f go.mod; and echo "Go"; and return
    test -f Cargo.toml; and echo "Rust"; and return
    test -f package.json; and echo "JavaScript"; and return
    test -f pyproject.toml; and echo "Python"; and return
    test -f requirements.txt; and echo "Python"; and return
    test -f Pipfile; and echo "Python"; and return
    test -f Gemfile; and echo "Ruby"; and return
    test -f pom.xml; and echo "Java"; and return
    test -f build.gradle; and echo "Java"; and return
    test -f CMakeLists.txt; and echo "C/C++"; and return
    test -f Makefile; and echo "C/C++"; and return
    count *.crl >/dev/null 2>&1; and echo "Carrion"; and return
    echo "None"
`

// ivaldiTimelineAwk extracts a "timeline: <name>" value; identical program to
// the one in defaultVCSDetect (no single quotes inside, so it can be wrapped
// in fish single quotes verbatim).
const ivaldiTimelineAwk = `tolower($1) ~ /^[[:space:]]*timeline[[:space:]]*$/ {sub(/^[[:space:]]+/, "", $2); gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit}`

// Fish translation of the default VCS-detect body (see defaultVCSDetect in
// config.go): Git branch + ahead/behind + staged/unstaged/untracked counts,
// then Ivaldi timeline + staged/unstaged/untracked counts.
const fishDefaultVCSDetect = `    # Detect VCS (Git + Ivaldi)
    set -l _vcs ""
    if git rev-parse --is-inside-work-tree >/dev/null 2>&1
        set -l _branch (git branch --show-current 2>/dev/null; or echo "?")

        set -l _ahead 0
        set -l _behind 0
        if git rev-parse --abbrev-ref "@{upstream}" >/dev/null 2>&1
            set -l _counts (git rev-list --left-right --count "HEAD...@{upstream}" 2>/dev/null | string split \t)
            if test (count $_counts) -ge 2
                string match -qr '^[0-9]+$' -- $_counts[1]; and set _behind $_counts[1]
                string match -qr '^[0-9]+$' -- $_counts[2]; and set _ahead $_counts[2]
            end
        end

        set -l _staged 0
        set -l _unstaged 0
        set -l _untracked 0
        git status --porcelain 2>/dev/null | while read -l _line
            if test (string sub -l 2 -- $_line) = "??"
                set _untracked (math $_untracked + 1)
            else
                set -l _x (string sub -l 1 -- $_line)
                set -l _y (string sub -s 2 -l 1 -- $_line)
                test "$_x" != " "; and set _staged (math $_staged + 1)
                test "$_y" != " "; and set _unstaged (math $_unstaged + 1)
            end
        end

        set -l _state ""
        test $_ahead -gt 0; and set _state "$_state ^$_ahead"
        test $_behind -gt 0; and set _state "$_state v$_behind"
        test $_staged -gt 0; and set _state "$_state +$_staged"
        test $_unstaged -gt 0; and set _state "$_state ~$_unstaged"
        test $_untracked -gt 0; and set _state "$_state ?$_untracked"

        if test -n "$_state"
            set _vcs "Git($_branch$_state)"
        else
            set _vcs "Git($_branch)"
        end
    end

    set -l _ivaldi_tl ""
    set -l _ivaldi_present ""
    set -l _ivaldi_state ""
    if command -q ivaldi
        set -l _ivaldi_raw (ivaldi whereami 2>/dev/null)
        if test -z "$_ivaldi_raw"
            set _ivaldi_raw (ivaldi wai 2>/dev/null)
        end
        if test -n "$_ivaldi_raw"
            set _ivaldi_present 1
        end
        set _ivaldi_tl (printf '%s\n' $_ivaldi_raw | awk -F: '` + ivaldiTimelineAwk + `')

        # Staged (gathered) / unstaged / untracked counts, like the Git ones.
        set -l _ivaldi_json (ivaldi status --json 2>/dev/null)
        if test -n "$_ivaldi_json"
            set _ivaldi_present 1
            set -l _icounts (printf '%s\n' $_ivaldi_json | awk '` + ivaldiStatusAwk + `' | string split ' ')
            if test (count $_icounts) -ge 3
                string match -qr '^[0-9]+$' -- $_icounts[1]; and test $_icounts[1] -gt 0; and set _ivaldi_state "$_ivaldi_state +$_icounts[1]"
                string match -qr '^[0-9]+$' -- $_icounts[2]; and test $_icounts[2] -gt 0; and set _ivaldi_state "$_ivaldi_state ~$_icounts[2]"
                string match -qr '^[0-9]+$' -- $_icounts[3]; and test $_icounts[3] -gt 0; and set _ivaldi_state "$_ivaldi_state ?$_icounts[3]"
            end
        end
    end
    if test -z "$_ivaldi_tl"; and test -f .ivaldi
        set _ivaldi_present 1
        set _ivaldi_tl (awk -F: '` + ivaldiTimelineAwk + ` NF{print; exit}' .ivaldi 2>/dev/null)
    end
    if test -z "$_ivaldi_tl"; and test -d .ivaldi
        set _ivaldi_present 1
        for _ivaldi_file in .ivaldi/timeline .ivaldi/whereami .ivaldi/wai
            if test -f $_ivaldi_file
                set _ivaldi_tl (awk -F: '` + ivaldiTimelineAwk + ` NF{print; exit}' $_ivaldi_file 2>/dev/null)
                test -n "$_ivaldi_tl"; and break
            end
        end
    end
    if test -n "$_ivaldi_tl"; or test -n "$_ivaldi_present"
        set -l _ivaldi_display "Ivaldi"
        if test -n "$_ivaldi_tl"
            set _ivaldi_display "Ivaldi (tl: $_ivaldi_tl$_ivaldi_state)"
        else if test -n "$_ivaldi_state"
            set _ivaldi_display "Ivaldi ("(string trim -- $_ivaldi_state)")"
        end
        if test -n "$_vcs"
            set _vcs "$_vcs | $_ivaldi_display"
        else
            set _vcs "$_ivaldi_display"
        end
    end

    test -z "$_vcs"; and set _vcs "None"
    echo $_vcs
`

// buildFishPromptFunction emits fish_prompt, mirroring buildPromptFunction's
// styles. fish has no PS1/PROMPT_COMMAND: the prompt is whatever fish_prompt
// prints, and the \[ \] zero-width markers of the bash version are dropped.
func (c *Config) buildFishPromptFunction() string {
	const (
		cyan    = `\033[0;36m`
		green   = `\033[0;32m`
		yellow  = `\033[0;33m`
		magenta = `\033[0;35m`
		blue    = `\033[0;34m`
		red     = `\033[0;31m`
		dim     = `\033[0;90m`
		reset   = `\033[0m`
	)

	distro := getDistroName()

	// $PWD with $HOME abbreviated to ~ (the equivalent of bash's \w).
	cwd := `    set -l _cwd (string replace -r -- '^'(string escape --style=regex -- $HOME)'($|/)' '~$1' $PWD)` + "\n"

	var b strings.Builder
	b.WriteString("# Prompt function\n")
	b.WriteString("function fish_prompt\n")

	style := c.Prompt.Style
	if style == "custom" {
		// Custom prompt scripts are bash (they set PS1); they cannot drive a
		// fish prompt. Fish keeps the informative full prompt instead of
		// degrading to a bare "> " — unless no custom script is set, where
		// bash also falls back to the minimal prompt.
		if c.Prompt.CustomPromptScript != "" {
			b.WriteString("    # Custom prompt scripts are bash-only; fish uses the full prompt.\n")
			style = "full"
		} else {
			style = "minimal"
		}
	}

	switch style {
	case "minimal":
		b.WriteString("    printf '> '\n")

	case "simple":
		b.WriteString(cwd)
		b.WriteString("    printf '" + cyan + "%s" + reset + " > ' $_cwd\n")

	case "full":
		fallthrough
	default:
		b.WriteString("    set -l _status $status\n")
		// Line 1: cwd | Lang: X | VCS: Y (segments per config; separators
		// resolved at generation time since the toggles are static).
		first := true
		if c.Prompt.ShowPath {
			b.WriteString(cwd)
			b.WriteString("    printf '" + cyan + "%s" + reset + "' $_cwd\n")
			first = false
		}
		if c.Prompt.ShowLanguage {
			if first {
				b.WriteString("    printf '" + blue + "Lang:" + reset + " " + yellow + "%s" + reset + "' (__raven_detect_lang)\n")
			} else {
				b.WriteString("    printf ' " + dim + " | " + blue + "Lang:" + reset + " " + yellow + "%s" + reset + "' (__raven_detect_lang)\n")
			}
			first = false
		}
		if c.Prompt.ShowVCS {
			if first {
				b.WriteString("    printf '" + blue + "VCS:" + reset + " " + magenta + "%s" + reset + "' (__raven_detect_vcs)\n")
			} else {
				b.WriteString("    printf ' " + dim + " | " + blue + "VCS:" + reset + " " + magenta + "%s" + reset + "' (__raven_detect_vcs)\n")
			}
			first = false
		}
		b.WriteString("    printf '\\n'\n")
		// Line 2: [user@distro] err:N >
		if c.Prompt.ShowUsername || c.Prompt.ShowHostname {
			b.WriteString("    printf '['\n")
			if c.Prompt.ShowUsername {
				b.WriteString("    printf '" + green + "%s" + reset + "' $USER\n")
			}
			if c.Prompt.ShowUsername && c.Prompt.ShowHostname {
				b.WriteString("    printf '@'\n")
			}
			if c.Prompt.ShowHostname {
				b.WriteString("    printf '" + yellow + "%s" + reset + "' " + FishQuote(distro) + "\n")
			}
			b.WriteString("    printf '] '\n")
		}
		b.WriteString("    if test $_status -ne 0\n")
		b.WriteString("        printf '" + red + "err:%s" + reset + " ' $_status\n")
		b.WriteString("    end\n")
		b.WriteString("    printf '" + dim + ">" + reset + " '\n")
	}

	b.WriteString("    __raven_emit_osc7\n")
	b.WriteString("end\n")
	return b.String()
}
