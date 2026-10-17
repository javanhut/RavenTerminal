package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseArgsWorkingDirectory(t *testing.T) {
	cases := []struct {
		args []string
		dir  string
	}{
		{nil, ""},
		{[]string{"--working-directory", "/tmp/a b"}, "/tmp/a b"},
		{[]string{"--working-directory=/srv"}, "/srv"},
		{[]string{"-d", "/home"}, "/home"},
		// After -e the arguments belong to the command, not to us.
		{[]string{"-e", "ls", "-d", "/etc"}, ""},
		{[]string{"-d", "/x", "-e", "nvim", "--working-directory", "/y"}, "/x"},
		// A trailing flag with no value is ignored rather than fatal.
		{[]string{"--working-directory"}, ""},
		// Positional DIR, plain or as a file:// URI (file managers pass these).
		{[]string{"/var/log"}, "/var/log"},
		{[]string{"file:///home/me/My%20Docs"}, "/home/me/My Docs"},
		{[]string{"file://localhost/srv/x"}, "/srv/x"},
		{[]string{"--hold", "/opt", "-e", "top"}, "/opt"},
	}
	for _, c := range cases {
		o, err := parseArgs(c.args)
		if err != nil {
			t.Errorf("parseArgs(%q): %v", c.args, err)
			continue
		}
		if o.dir != c.dir {
			t.Errorf("parseArgs(%q).dir = %q, want %q", c.args, o.dir, c.dir)
		}
	}
}

func TestParseArgsCommand(t *testing.T) {
	o, _ := parseArgs([]string{"--working-directory", "/tmp"})
	if o.command != nil {
		t.Errorf("command = %q, want nil", o.command)
	}
	for _, args := range [][]string{
		{"-d", "/tmp", "-e", "nvim", "x"},
		{"--execute", "nvim", "x"},
		{"--hold", "--", "nvim", "x"},
	} {
		o, err := parseArgs(args)
		if want := []string{"nvim", "x"}; err != nil || !reflect.DeepEqual(o.command, want) {
			t.Errorf("parseArgs(%q).command = %q (%v), want %q", args, o.command, err, want)
		}
	}
}

func TestParseArgsFlags(t *testing.T) {
	o, err := parseArgs([]string{"--hold", "--title", "Build log", "--class=dev-term", "-e", "make", "--title", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.hold || o.title != "Build log" || o.class != "dev-term" {
		t.Fatalf("got %+v", o)
	}
	if !reflect.DeepEqual(o.command, []string{"make", "--title", "x"}) {
		t.Fatalf("command = %q", o.command)
	}
	o, _ = parseArgs([]string{"--app-id", "scratch", "--title=T"})
	if o.class != "scratch" || o.title != "T" {
		t.Fatalf("got %+v", o)
	}
	o, _ = parseArgs(nil)
	if o.class != "raven-terminal" || o.hold || o.title != "" {
		t.Fatalf("defaults = %+v", o)
	}
	o, _ = parseArgs([]string{"--list-fonts"})
	if !o.listFonts {
		t.Fatal("--list-fonts not parsed")
	}
	o, _ = parseArgs([]string{"--keybindings"})
	if !o.keybindings {
		t.Fatal("--keybindings not parsed")
	}
}

func TestParseArgsRejectsUnknown(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"/a", "/b"}} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) accepted bad input", args)
		}
	}
}

func TestInfoOutputs(t *testing.T) {
	fonts := listFontsText()
	if !strings.Contains(fonts, "jetbrains") || !strings.Contains(fonts, "firacode") {
		t.Fatalf("--list-fonts output lacks fonts:\n%s", fonts)
	}
	keys := keybindingsText()
	if !strings.Contains(keys, "Keybindings") || !strings.Contains(keys, "New tab") {
		t.Fatalf("--keybindings output:\n%s", keys)
	}
	if u := usageText(); !strings.Contains(u, "--hold") || !strings.Contains(u, "--app-id") {
		t.Fatalf("usage lacks flags:\n%s", u)
	}
}

func TestParseArgsVersion(t *testing.T) {
	for _, arg := range []string{"-V", "--version"} {
		o, err := parseArgs([]string{arg})
		if err != nil || !o.version {
			t.Errorf("parseArgs(%q) = version %v, err %v; want version", arg, o.version, err)
		}
	}
}
