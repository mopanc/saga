// saga-hook — Claude Code UserPromptSubmit hook.
// Phase 1 scaffold: drains stdin, no-op output.
// Recall + cwd-aware layer resolution land next.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/jorgemorais/saga/internal/saga"
)

func main() {
	// Always drain stdin so the parent doesn't block on a broken pipe.
	_, _ = io.ReadAll(os.Stdin)
	fmt.Fprintf(os.Stderr, "saga-hook v%s — scaffold; recall pending\n", saga.Version)
}
