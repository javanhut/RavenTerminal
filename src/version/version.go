// Package version is the terminal's build version: TERM_PROGRAM_VERSION,
// the XTVERSION reply and --version all report String.
package version

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

// base is the release version. It lives in VERSION beside this file so a
// plain `go build ./src`, which is what rvn's package recipe runs, still
// reports it without any linker flags.
//
//go:embed VERSION
var base string

// Stamp, set at link time, overrides everything else. A build that knows
// the revision it compiled uses it to say so:
//
//	-ldflags "-X github.com/javanhut/RavenTerminal/src/version.Stamp=0.1.0+r20261016.gabc123"
var Stamp string

// readBuildInfo is swapped out by tests.
var readBuildInfo = debug.ReadBuildInfo

// String returns Stamp when set, else the module version that
// `go install github.com/javanhut/RavenTerminal/src@vX` records, else VERSION.
// A checkout build records "(devel)", which says nothing, so it falls
// through to VERSION.
func String() string {
	if s := strings.TrimSpace(Stamp); s != "" {
		return s
	}
	if info, ok := readBuildInfo(); ok {
		if v := strings.TrimPrefix(info.Main.Version, "v"); v != "" && v != "(devel)" {
			return v
		}
	}
	return strings.TrimSpace(base)
}
