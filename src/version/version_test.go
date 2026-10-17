package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func withBuildInfo(t *testing.T, mainVersion string, ok bool) {
	t.Helper()
	orig := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}, ok
	}
	t.Cleanup(func() { readBuildInfo = orig })
}

func withStamp(t *testing.T, s string) {
	t.Helper()
	orig := Stamp
	Stamp = s
	t.Cleanup(func() { Stamp = orig })
}

func TestStringPrecedence(t *testing.T) {
	want := strings.TrimSpace(base)
	cases := []struct {
		name, stamp, module string
		ok                  bool
		want                string
	}{
		{"checkout build uses VERSION", "", "(devel)", true, want},
		{"no build info uses VERSION", "", "", false, want},
		{"go install records module version", "", "v1.2.3", true, "1.2.3"},
		{"stamp beats module version", "0.1.0+r20261016.gabc", "v1.2.3", true, "0.1.0+r20261016.gabc"},
		{"blank stamp is ignored", "  ", "(devel)", true, want},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withStamp(t, c.stamp)
			withBuildInfo(t, c.module, c.ok)
			if got := String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVersionFileIsOneLine(t *testing.T) {
	if base == "" || base[len(base)-1] != '\n' {
		t.Fatalf("VERSION = %q, want one newline-terminated line", base)
	}
	for _, r := range base[:len(base)-1] {
		if r == '\n' || r == ' ' {
			t.Fatalf("VERSION = %q, want a bare version string", base)
		}
	}
}
