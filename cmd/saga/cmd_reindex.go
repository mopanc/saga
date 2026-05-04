package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jorgemorais/saga/internal/saga"
)

func runReindex(args []string) error {
	fs := flag.NewFlagSet("reindex", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga reindex — rebuild SQLite index from markdown in active layers (personal + auto-discovered project).")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

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
