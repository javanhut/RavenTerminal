package main

import (
	"reflect"
	"testing"
)

func TestStartDirFromArgs(t *testing.T) {
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
	}
	for _, c := range cases {
		if got := startDirFromArgs(c.args); got != c.dir {
			t.Errorf("startDirFromArgs(%q) = %q, want %q", c.args, got, c.dir)
		}
	}
}

func TestWorkingDirectoryDoesNotBecomeTheCommand(t *testing.T) {
	if got := commandFromArgs([]string{"--working-directory", "/tmp"}); got != nil {
		t.Errorf("commandFromArgs = %q, want nil", got)
	}
	got := commandFromArgs([]string{"-d", "/tmp", "-e", "nvim", "x"})
	if want := []string{"nvim", "x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("commandFromArgs = %q, want %q", got, want)
	}
}
