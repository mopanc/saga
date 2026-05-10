package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/mopanc/saga/internal/saga"
)

// runConflicts prints all unresolved @conflicts_with pairs in active layers.
// Pairs are deduplicated regardless of which side declared the relation.
func runConflicts(args []string) error {
	fs := flag.NewFlagSet("conflicts", flag.ExitOnError)
	format := fs.String("format", "human", "output format: human|json")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga conflicts — list topic pairs linked by @conflicts_with.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := saga.LoadConfig()
	if err != nil {
		return err
	}
	db, err := saga.OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	cwd, _ := os.Getwd()
	svc := saga.NewService(db, cfg, cwd)

	pairs, err := svc.ListConflicts()
	if err != nil {
		return err
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Pairs []saga.ConflictPair `json:"pairs"`
		}{Pairs: pairs})
	case "human":
		if len(pairs) == 0 {
			fmt.Println("No @conflicts_with pairs in active layers.")
			return nil
		}
		fmt.Printf("%d conflict pair(s):\n\n", len(pairs))
		for i, p := range pairs {
			fmt.Printf("%d. %s\n   ⇄ %s\n", i+1, p.A.Title, p.B.Title)
			fmt.Printf("   scopes: %s | %s\n", p.A.Scope, p.B.Scope)
			if p.Note != "" {
				fmt.Printf("   note: %s\n", p.Note)
			}
			fmt.Println()
		}
		fmt.Println("Resolve with `saga show <id>` to inspect each side, or with @supersedes / @refines once the canonical version is clear.")
		return nil
	default:
		return fmt.Errorf("unknown format %q (want human|json)", *format)
	}
}
