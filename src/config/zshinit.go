package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Zsh init script generation. zsh has no --rcfile, so Raven uses the standard
// ZDOTDIR shim: shell/pty.go points ZDOTDIR at the generated zsh/ directory,
// whose .zshenv chains to the user's real ~/.zshenv, restores ZDOTDIR so
// .zprofile/.zshrc load from their usual location, and sources raven-init.zsh
// — a native zsh translation of init.sh (init.sh is bash: its PROMPT_COMMAND
// and \[ \] prompt markers do not work in zsh).

// ZshDotDir returns the directory used as ZDOTDIR for zsh sessions.
func ZshDotDir() string {
	return filepath.Join(GetScriptsDir(), "zsh")
}

// ZshInitPath returns the path of the generated zsh init script.
func ZshInitPath() string {
	return filepath.Join(ZshDotDir(), "raven-init.zsh")
}

// zshQuote quotes s as a zsh single-quoted string.
func zshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// zshDetectFunction emits a zsh detect function backed by a bash helper
// script (the detect bodies in config are bash syntax; running them through
// bash matches the fish/rsh bridges and avoids subtle zsh incompatibilities
// like nomatch aborting on `ls *.crl`).
func zshDetectFunction(name, scriptPath string) string {
	var b strings.Builder
	b.WriteString(name + "() {\n")
	if scriptPath == "" || strings.ContainsAny(scriptPath, `"'`) {
		b.WriteString("    echo 'None'\n")
	} else {
		b.WriteString("    if command -v bash >/dev/null 2>&1; then\n")
		b.WriteString("        bash \"" + scriptPath + "\"\n")
		b.WriteString("    else\n")
		b.WriteString("        echo 'None'\n")
		b.WriteString("    fi\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// writeZshInitScript writes the ZDOTDIR shim (.zshenv) and the zsh init
// script, returning the init script path.
func (c *Config) writeZshInitScript() (string, error) {
	dir := ZshDotDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	initPath := ZshInitPath()

	var e strings.Builder
	e.WriteString("# Raven Terminal zsh bootstrap (ZDOTDIR shim) - Auto-generated\n")
	e.WriteString("# Do not edit directly - changes will be overwritten\n\n")
	e.WriteString("# Recursion guard: apply the shim once per shell tree.\n")
	e.WriteString("if [ -z \"$RAVEN_ZDOTDIR_DONE\" ]; then\n")
	e.WriteString("    export RAVEN_ZDOTDIR_DONE=1\n")
	e.WriteString("    if [ -n \"$RAVEN_ZSH_NORC\" ]; then\n")
	e.WriteString("        # rc sourcing disabled: skip global rcs; ZDOTDIR stays here\n")
	e.WriteString("        # (this dir has no .zprofile/.zshrc), so user rcs are skipped.\n")
	e.WriteString("        setopt no_global_rcs\n")
	e.WriteString("    else\n")
	e.WriteString("        # Load the user's real ~/.zshenv, then restore ZDOTDIR so\n")
	e.WriteString("        # .zprofile/.zshrc load from their usual location.\n")
	e.WriteString("        [ -f \"$HOME/.zshenv\" ] && builtin source \"$HOME/.zshenv\"\n")
	e.WriteString("        if [ \"$ZDOTDIR\" = " + zshQuote(dir) + " ]; then\n")
	e.WriteString("            export ZDOTDIR=\"$HOME\"\n")
	e.WriteString("        fi\n")
	e.WriteString("    fi\n")
	e.WriteString("    builtin source " + zshQuote(initPath) + "\n")
	e.WriteString("fi\n")
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte(e.String()), 0644); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Raven Terminal Init Script (zsh) - Auto-generated\n")
	b.WriteString("# Do not edit directly - changes will be overwritten\n")
	b.WriteString("# Edit config.toml instead\n\n")

	// User init script. Kept in its own sub-sourced file so a syntax error in
	// it aborts only that file, not the prompt/detect functions below (same
	// isolation as init.fish).
	if strings.TrimSpace(c.Scripts.Init) != "" {
		userPath := filepath.Join(dir, "raven-user-init.zsh")
		if err := os.WriteFile(userPath, []byte(c.Scripts.Init+"\n"), 0644); err == nil {
			b.WriteString("# User init script (kept in its own file so a syntax error\n")
			b.WriteString("# cannot break the rest of this script)\n")
			b.WriteString("builtin source " + zshQuote(userPath) + "\n\n")
		}
	}

	// Detect functions reuse the bash helper scripts also written for the rsh
	// init (identical content and paths, so double-writing is harmless).
	b.WriteString("# Language detection function\n")
	b.WriteString(zshDetectFunction("__raven_detect_lang",
		writeRavenDetectScript("raven-lang-detect", c.Scripts.LanguageDetect)))
	b.WriteString("\n")

	b.WriteString("# VCS detection function\n")
	b.WriteString(zshDetectFunction("__raven_detect_vcs",
		writeRavenDetectScript("raven-vcs-detect", c.Scripts.VCSDetect)))
	b.WriteString("\n")

	b.WriteString("# Emit OSC 7 for current working directory\n")
	b.WriteString("__raven_emit_osc7() {\n")
	b.WriteString("    local _host=\"${HOST:-$(hostname 2>/dev/null)}\"\n")
	b.WriteString("    printf '\\033]7;file://%s%s\\a' \"$_host\" \"$PWD\"\n")
	b.WriteString("}\n\n")

	b.WriteString(c.buildZshPromptFunction())

	// Aliases (sorted for deterministic output).
	if len(c.Aliases) > 0 {
		b.WriteString("\n# Aliases\n")
		names := make([]string, 0, len(c.Aliases))
		for name := range c.Aliases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b.WriteString("alias " + name + "=" + zshQuote(c.Aliases[name]) + "\n")
		}
	}

	// Exports (same emission as init.sh; zsh expands "$VAR"/"${VAR}" alike).
	if len(c.Exports) > 0 {
		b.WriteString("\n# Exports\n")
		names := make([]string, 0, len(c.Exports))
		for name := range c.Exports {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b.WriteString("export " + name + "=\"" + bashDoubleQuote(c.Exports[name]) + "\"\n")
		}
	}

	// PATH additions (the POSIX case-guard from init.sh is valid zsh).
	if len(c.Shell.Paths) > 0 {
		b.WriteString("\n# Raven PATH additions\n")
		for _, dir := range c.Shell.Paths {
			d := bashDoubleQuote(dir)
			b.WriteString("case \":$PATH:\" in *\":" + d + ":\"*) ;; *) PATH=\"" + d + ":$PATH\";; esac\n")
		}
		b.WriteString("export PATH\n")
	}

	if err := os.WriteFile(initPath, []byte(b.String()), 0644); err != nil {
		return "", err
	}
	return initPath, nil
}

// buildZshPromptFunction emits a precmd hook that rebuilds PS1 before every
// prompt, mirroring buildPromptFunction's styles with native zsh %-escapes
// (%~ cwd, %n user, %F{color}) instead of bash's \[ \] markers.
func (c *Config) buildZshPromptFunction() string {
	const (
		cyan    = "%F{cyan}"
		green   = "%F{green}"
		yellow  = "%F{yellow}"
		magenta = "%F{magenta}"
		blue    = "%F{blue}"
		red     = "%F{red}"
		dim     = "%F{8}"
		reset   = "%f"
	)

	distro := getDistroName()

	var b strings.Builder
	b.WriteString("# Prompt (precmd hook; runs before every prompt like the bash version)\n")
	b.WriteString("__raven_precmd() {\n")
	b.WriteString("    local _status=$?\n")

	style := c.Prompt.Style
	if style == "custom" {
		// Custom prompt scripts are bash (they set a bash PS1); zsh keeps the
		// informative full prompt instead — unless no custom script is set,
		// where bash also falls back to the minimal prompt.
		if c.Prompt.CustomPromptScript != "" {
			b.WriteString("    # Custom prompt scripts are bash-only; zsh uses the full prompt.\n")
			style = "full"
		} else {
			style = "minimal"
		}
	}

	switch style {
	case "minimal":
		b.WriteString("    PS1='> '\n")

	case "simple":
		b.WriteString("    PS1='" + cyan + "%~" + reset + " > '\n")

	case "full":
		fallthrough
	default:
		// Line 1: cwd | Lang: X | VCS: Y (per config toggles). Dynamic detect
		// output has its % doubled so it stays literal in PS1.
		b.WriteString("    local _line1=\"\"\n")
		if c.Prompt.ShowPath {
			b.WriteString("    _line1=\"" + cyan + "%~" + reset + "\"\n")
		}
		if c.Prompt.ShowLanguage {
			b.WriteString("    local _lang=\"$(__raven_detect_lang)\"\n")
			b.WriteString("    _lang=\"${_lang//\\%/%%}\"\n")
			b.WriteString("    if [ -n \"$_line1\" ]; then\n")
			b.WriteString("        _line1=\"$_line1 " + dim + " | " + blue + "Lang:" + reset + " " + yellow + "${_lang}" + reset + "\"\n")
			b.WriteString("    else\n")
			b.WriteString("        _line1=\"" + blue + "Lang:" + reset + " " + yellow + "${_lang}" + reset + "\"\n")
			b.WriteString("    fi\n")
		}
		if c.Prompt.ShowVCS {
			b.WriteString("    local _vcs=\"$(__raven_detect_vcs)\"\n")
			b.WriteString("    _vcs=\"${_vcs//\\%/%%}\"\n")
			b.WriteString("    if [ -n \"$_line1\" ]; then\n")
			b.WriteString("        _line1=\"$_line1 " + dim + " | " + blue + "VCS:" + reset + " " + magenta + "${_vcs}" + reset + "\"\n")
			b.WriteString("    else\n")
			b.WriteString("        _line1=\"" + blue + "VCS:" + reset + " " + magenta + "${_vcs}" + reset + "\"\n")
			b.WriteString("    fi\n")
		}

		// Line 2: [user@distro] err:N >
		b.WriteString("    local _line2=\"\"\n")
		if c.Prompt.ShowUsername || c.Prompt.ShowHostname {
			b.WriteString("    _line2=\"[\"\n")
			if c.Prompt.ShowUsername {
				b.WriteString("    _line2=\"${_line2}" + green + "%n" + reset + "\"\n")
			}
			if c.Prompt.ShowUsername && c.Prompt.ShowHostname {
				b.WriteString("    _line2=\"${_line2}@\"\n")
			}
			if c.Prompt.ShowHostname {
				b.WriteString("    _line2=\"${_line2}" + yellow + distro + reset + "\"\n")
			}
			b.WriteString("    _line2=\"${_line2}] \"\n")
		}
		b.WriteString("    if [ \"$_status\" -ne 0 ]; then\n")
		b.WriteString("        _line2=\"${_line2}" + red + "err:${_status}" + reset + " \"\n")
		b.WriteString("    fi\n")
		b.WriteString("    _line2=\"${_line2}" + dim + ">" + reset + " \"\n")
		b.WriteString("    PS1=\"${_line1}\"$'\\n'\"${_line2}\"\n")
	}

	b.WriteString("    __raven_emit_osc7\n")
	b.WriteString("}\n")
	b.WriteString("if [[ -z ${precmd_functions[(r)__raven_precmd]} ]]; then\n")
	b.WriteString("    precmd_functions+=(__raven_precmd)\n")
	b.WriteString("fi\n")
	return b.String()
}
