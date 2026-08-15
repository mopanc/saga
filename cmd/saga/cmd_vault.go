package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mopanc/saga/internal/saga"
)

func runVault(args []string) error {
	fs := flag.NewFlagSet("vault", flag.ExitOnError)
	path := fs.String("path", "", "where to build the vault (default: <saga home>/vault)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `saga vault — assemble every layer into one directory you can open in Obsidian.

  saga vault                 build (or refresh) the vault
  saga vault --path <dir>    build it somewhere else

Creates one symlink per layer, so the notes stay where they are and saga
remains the only writer of record. Editing a note in Obsidian edits the real
file. Re-run after adding a project layer.`)
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

	root := *path
	if root == "" {
		root = filepath.Join(cfg.HomeDir, "vault")
	}

	res, err := saga.NewService(db, cfg, cwd).BuildVault(root)
	if err != nil {
		return err
	}
	if len(res.Links) == 0 {
		fmt.Println("no layers to link — index is empty (run `saga reindex` first)")
		return nil
	}

	total := 0
	for _, l := range res.Links {
		state := "ok"
		if l.Created {
			state = "linked"
		}
		fmt.Printf("  %-8s %-20s %3d notes  -> %s\n", state, l.Scope, l.Notes, l.Target)
		total += l.Notes
	}
	fmt.Printf("\n%d layer(s), %d notes\n", len(res.Links), total)
	fmt.Printf("\nOpen in Obsidian:  \"Open folder as vault\" -> %s\n", res.Root)
	fmt.Println("Then turn on Settings -> Files & Links -> \"Detect all file extensions\" if you")
	fmt.Println("want to see non-markdown files, and open the graph view.")
	return nil
}
