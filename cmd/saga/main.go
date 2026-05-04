// saga — CLI for Saga.
// Phase 1 scaffold: version + reindex stub.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jorgemorais/saga/internal/saga"
)

const usage = `saga v%s — AI investigation memory

Usage:
  saga <command>

Commands:
  version     Print version
  reindex     Rebuild SQLite index from markdown files (stub)
`

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, usage, saga.Version)
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	switch args[0] {
	case "version":
		fmt.Printf("saga v%s\n", saga.Version)

	case "reindex":
		cfg, err := saga.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "saga: config: %v\n", err)
			os.Exit(1)
		}
		db, err := saga.OpenDB(cfg.DBPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "saga: open db: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
		fmt.Printf("saga reindex: db open at %s. (reindex impl pending)\n", cfg.DBPath)

	default:
		fmt.Fprintf(os.Stderr, "saga: unknown command %q\n\n", args[0])
		flag.Usage()
		os.Exit(2)
	}
}
