// saga — CLI for Saga.
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
  reindex     Rebuild SQLite index from markdown files in active layers
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
		if err := runReindex(); err != nil {
			fmt.Fprintf(os.Stderr, "saga reindex: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "saga: unknown command %q\n\n", args[0])
		flag.Usage()
		os.Exit(2)
	}
}

func runReindex() error {
	cfg, err := saga.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	db, err := saga.OpenDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	resolver := saga.NewResolver(cfg)
	layers, err := resolver.Resolve(cwd)
	if err != nil {
		return fmt.Errorf("resolve layers: %w", err)
	}

	totalIndexed, totalFailed := 0, 0
	for _, layer := range layers {
		result, err := db.IndexLayer(layer)
		if err != nil {
			return fmt.Errorf("index %s: %w", layer.Scope, err)
		}
		fmt.Printf("%-20s indexed=%d failed=%d  (%s)\n",
			layer.Scope, result.Indexed, result.Failed, layer.NotesDir)
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", e.File, e.Err)
		}
		totalIndexed += result.Indexed
		totalFailed += result.Failed
	}
	fmt.Printf("done — %d layer(s), %d indexed, %d failed\n", len(layers), totalIndexed, totalFailed)
	return nil
}
