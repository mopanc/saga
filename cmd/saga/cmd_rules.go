package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mopanc/saga/internal/saga"
)

func runRules(args []string) error {
	fs := flag.NewFlagSet("rules", flag.ExitOnError)
	budget := fs.Int("budget", 0, "render as the hook would, capped at this token budget (0 = no cap, list everything)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `saga rules — list the policy notes in force for the current directory.

  saga rules              every rule across the active layers, no budget applied
  saga rules --budget 700 exactly what the always-on catalogue injects at that budget

The always-on catalogue lists rules by name and trigger phrases only; read a
rule in full with 'saga show <id>' before acting on it.`)
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

	// A very large budget stands in for "no cap" — the catalogue drops entries
	// only when they do not fit, so an unreachable budget lists everything.
	tokens := *budget
	if tokens <= 0 {
		tokens = 1 << 20
	}

	out, ids, err := saga.NewService(db, cfg, cwd).BuildRuleCatalog(tokens)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("no policy notes in the active layers")
		return nil
	}
	fmt.Println(out)
	return nil
}
