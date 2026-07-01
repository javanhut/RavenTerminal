package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RavenShell (rsh) init script generation. RavenShell cannot source init.sh
// (it is a Go-like scripting language, not POSIX), so it gets its own
// translation, written alongside init.sh and loaded by ravenshell itself via
// the RAVEN_INIT_SCRIPT environment variable (see shell/pty.go). The script
// defines a `prompt` function that RavenShell's REPL calls before each read.
//
// rsh strings have no backslash escapes, so ANSI color codes are embedded as
// raw ESC bytes. The detect bodies (config [scripts] values) are bash syntax,
// so they are written to small helper .sh files and invoked through bash.

// RavenInitPath returns the path of the generated rsh init script.
func RavenInitPath() string {
	return filepath.Join(GetScriptsDir(), "init.rsh")
}

// ravenDetectScriptPath returns the path of a bash helper script backing an
// rsh detect function.
func ravenDetectScriptPath(name string) string {
	return filepath.Join(GetScriptsDir(), name+".sh")
}

// rshQuote renders s as an rsh string literal. rsh has no escape sequences:
// double-quoted strings interpolate $VAR and cannot contain a double quote;
// single-quoted strings are fully literal and cannot contain a single quote.
// It returns ok=false when s contains both quote characters and therefore
// cannot be represented at all.
func rshQuote(s string, interpolate bool) (string, bool) {
	if interpolate || strings.Contains(s, "'") {
		if strings.Contains(s, `"`) {
			return "", false
		}
		return `"` + s + `"`, true
	}
	return "'" + s + "'", true
}

// writeRavenDetectScript writes a detect body (bash syntax) to a helper
// script, wrapped in a function so its `return` statements work. It returns
// the script path, or "" when the body is empty or the write fails.
func writeRavenDetectScript(name, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	path := ravenDetectScriptPath(name)
	script := "#!/bin/bash\n" +
		"# Raven Terminal detect helper - Auto-generated, do not edit\n" +
		"__raven_fn() {\n" + body + "\n}\n__raven_fn\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		return ""
	}
	return path
}

// rshDetectFunction emits an rsh detect function backed by a bash helper
// script (or a literal 'None' when no body is configured).
func rshDetectFunction(name, scriptPath string) string {
	var b strings.Builder
	b.WriteString("fn " + name + "() {\n")
	if scriptPath == "" || strings.ContainsAny(scriptPath, `"'`) {
		b.WriteString("    return \"None\"\n")
	} else {
		b.WriteString("    return $(bash \"" + scriptPath + "\")\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// writeRavenInitScript writes the rsh variant of the init script and returns
// its path.
func (c *Config) writeRavenInitScript() (string, error) {
	path := RavenInitPath()
	var b strings.Builder

	b.WriteString("# Raven Terminal Init Script (rsh) - Auto-generated\n")
	b.WriteString("# Do not edit directly - changes will be overwritten\n")
	b.WriteString("# Edit config.toml instead\n\n")

	// The shared [scripts] init value is bash syntax (the settings menu
	// documents it that way); run it through bash rather than inlining it.
	if strings.TrimSpace(c.Scripts.Init) != "" {
		if userPath := writeRavenDetectScript("raven-user-init", c.Scripts.Init); userPath != "" {
			b.WriteString("# User init script (bash syntax, run through bash)\n")
			b.WriteString("bash \"" + userPath + "\"\n\n")
		}
	}

	b.WriteString("# Language detection function\n")
	langScript := writeRavenDetectScript("raven-lang-detect", c.Scripts.LanguageDetect)
	b.WriteString(rshDetectFunction("__raven_detect_lang", langScript))
	b.WriteString("\n")

	b.WriteString("# VCS detection function\n")
	vcsScript := writeRavenDetectScript("raven-vcs-detect", c.Scripts.VCSDetect)
	b.WriteString(rshDetectFunction("__raven_detect_vcs", vcsScript))
	b.WriteString("\n")

	b.WriteString("# Current directory with $HOME abbreviated to ~\n")
	b.WriteString("fn __raven_cwd() {\n")
	b.WriteString("    c = $(cwd)\n")
	b.WriteString("    h = $HOME\n")
	b.WriteString("    if c == h {\n")
	b.WriteString("        return \"~\"\n")
	b.WriteString("    }\n")
	b.WriteString("    return replace(c, h + \"/\", \"~/\")\n")
	b.WriteString("}\n\n")

	b.WriteString(c.buildRavenPromptFunction())

	// rsh has no alias mechanism, and PATH additions come in through the PTY
	// environment (shell/pty.go), so neither section is emitted here.

	// Exports.
	if len(c.Exports) > 0 {
		b.WriteString("\n# Exports\n")
		for _, name := range sortedKeys(c.Exports) {
			// rsh double quotes interpolate $VAR like the bash export does.
			if quoted, ok := rshQuote(c.Exports[name], true); ok {
				b.WriteString("export " + name + " " + quoted + "\n")
			}
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// sortedKeys returns the map's keys sorted for deterministic output.
func sortedKeys(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// buildRavenPromptFunction emits the rsh `prompt` function, mirroring
// buildPromptFunction's styles. RavenShell calls prompt(status) before each
// REPL read; the returned string is the prompt, and everything above its last
// newline is printed once per read (so the two-line full style works).
func (c *Config) buildRavenPromptFunction() string {
	const (
		esc     = "\x1b"
		bel     = "\a"
		cyan    = esc + "[0;36m"
		green   = esc + "[0;32m"
		yellow  = esc + "[0;33m"
		magenta = esc + "[0;35m"
		blue    = esc + "[0;34m"
		red     = esc + "[0;31m"
		dim     = esc + "[0;90m"
		reset   = esc + "[0m"
	)

	distro := getDistroName()

	var b strings.Builder
	b.WriteString("# Prompt function (drives the RavenShell REPL prompt)\n")

	style := c.Prompt.Style
	if style == "custom" {
		// Custom prompt scripts are bash (they set PS1); they cannot drive an
		// rsh prompt. Keep the informative full prompt unless no custom script
		// is set, where bash also falls back to the minimal prompt.
		if c.Prompt.CustomPromptScript != "" {
			b.WriteString("# Custom prompt scripts are bash-only; rsh uses the full prompt.\n")
			style = "full"
		} else {
			style = "minimal"
		}
	}

	switch style {
	case "minimal":
		b.WriteString("fn prompt() {\n")
		b.WriteString("    return \"> \"\n")
		b.WriteString("}\n")

	case "simple":
		b.WriteString("fn prompt() {\n")
		b.WriteString("    return \"" + cyan + "\" + __raven_cwd() + \"" + reset + " > \"\n")
		b.WriteString("}\n")

	case "full":
		fallthrough
	default:
		b.WriteString("fn prompt(status) {\n")
		// RavenShell already emits the OSC 7 cwd report natively (oscWorkingDir
		// in main.go), so the prompt script only needs the visible line.
		b.WriteString("    line1 = \"\"\n")
		first := true
		if c.Prompt.ShowPath {
			b.WriteString("    line1 = line1 + \"" + cyan + "\" + __raven_cwd() + \"" + reset + "\"\n")
			first = false
		}
		if c.Prompt.ShowLanguage {
			sep := ""
			if !first {
				sep = " " + dim + " | "
			}
			b.WriteString("    line1 = line1 + \"" + sep + blue + "Lang:" + reset + " " + yellow + "\" + __raven_detect_lang() + \"" + reset + "\"\n")
			first = false
		}
		if c.Prompt.ShowVCS {
			sep := ""
			if !first {
				sep = " " + dim + " | "
			}
			b.WriteString("    line1 = line1 + \"" + sep + blue + "VCS:" + reset + " " + magenta + "\" + __raven_detect_vcs() + \"" + reset + "\"\n")
		}
		b.WriteString("    line2 = \"\"\n")
		if c.Prompt.ShowUsername || c.Prompt.ShowHostname {
			b.WriteString("    line2 = \"[\"\n")
			if c.Prompt.ShowUsername {
				b.WriteString("    line2 = line2 + \"" + green + "\" + $USER + \"" + reset + "\"\n")
			}
			if c.Prompt.ShowUsername && c.Prompt.ShowHostname {
				b.WriteString("    line2 = line2 + \"@\"\n")
			}
			if c.Prompt.ShowHostname {
				b.WriteString("    line2 = line2 + \"" + yellow + distro + reset + "\"\n")
			}
			b.WriteString("    line2 = line2 + \"] \"\n")
		}
		b.WriteString("    if status != 0 {\n")
		b.WriteString("        line2 = line2 + \"" + red + "err:\" + status + \"" + reset + " \"\n")
		b.WriteString("    }\n")
		b.WriteString("    line2 = line2 + \"" + dim + ">" + reset + " \"\n")
		// A literal newline inside the string joins the two prompt lines.
		b.WriteString("    return line1 + \"\n\" + line2\n")
		b.WriteString("}\n")
	}

	return b.String()
}
