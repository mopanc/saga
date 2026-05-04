// saga-mcp — MCP stdio server for Saga.
// Phase 1 scaffold: opens DB, applies migrations, exits.
// Real MCP handlers (recall, topic.read/write/list/promote) land next.
package main

import (
	"fmt"
	"os"

	"github.com/jorgemorais/saga/internal/saga"
)

func main() {
	cfg, err := saga.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "saga-mcp: config: %v\n", err)
		os.Exit(1)
	}

	db, err := saga.OpenDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "saga-mcp: open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Fprintf(os.Stderr,
		"saga-mcp v%s — scaffold\nDB: %s\nMCP handlers pending. Exiting.\n",
		saga.Version, cfg.DBPath,
	)
}
