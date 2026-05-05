package saga

// Version is set via ldflags at build time by goreleaser:
//
//	-X github.com/mopanc/saga/internal/saga.Version={{.Tag}}
//
// Local builds keep the dev placeholder.
var Version = "0.2.0-dev"
