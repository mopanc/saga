package saga

import (
	"runtime/debug"
	"strings"
)

// Version is set via ldflags at build time by goreleaser:
//
//	-X github.com/mopanc/saga/internal/saga.Version={{.Version}}
//
// Local builds keep the dev placeholder. For `go install ...@vX.Y.Z` builds
// (no ldflag), VersionString falls back to the module version reported by
// runtime/debug.BuildInfo.
var Version = "0.2.0-dev"

// VersionString returns the displayable version, never with a leading "v"
// (callers prefix with "v" themselves).
//
// Resolution order:
//  1. ldflag-injected Version (goreleaser builds);
//  2. module version from runtime/debug.BuildInfo (`go install ...@vX.Y.Z`);
//  3. dev placeholder.
func VersionString() string {
	if Version != "0.2.0-dev" {
		return strings.TrimPrefix(Version, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return Version
}
