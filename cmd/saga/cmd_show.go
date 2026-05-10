package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/mopanc/saga/internal/saga"
)

// runShow prints a topic plus the graph of relations centred on it.
// Useful when chasing supersedes / refines / conflicts chains, and the
// affordance referenced by S0-3 + S0-5 acceptance criteria.
func runShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	format := fs.String("format", "human", "output format: human|json")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga show <id-or-slug-or-title> — print a topic with its outgoing and incoming relations.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("missing id-or-slug")
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

	res, err := svc.Show(fs.Arg(0))
	if err != nil {
		return err
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	case "human":
		t := res.Topic
		fmt.Printf("%s\n", t.Title)
		fmt.Printf("  id:         %s\n", t.ID)
		fmt.Printf("  scope:      %s\n", t.Scope)
		fmt.Printf("  type:       %s\n", t.Type)
		fmt.Printf("  confidence: %s\n", t.Confidence)
		if len(t.Synonyms) > 0 {
			fmt.Printf("  synonyms:   %v\n", t.Synonyms)
		}
		if len(res.Relations) > 0 {
			fmt.Println("\nrelations:")
			for _, r := range res.Relations {
				arrow := "→"
				if r.Direction == "in" {
					arrow = "←"
				}
				other := r.OtherID
				if r.Other != nil {
					other = r.Other.Title + " (" + r.Other.Scope + ")"
				} else {
					other += " [dangling]"
				}
				note := ""
				if r.Note != "" {
					note = " — " + r.Note
				}
				fmt.Printf("  %s %s %s%s\n", arrow, r.Op, other, note)
			}
		}
		fmt.Println("\nbody:")
		fmt.Println("---")
		fmt.Println(t.Body)
		return nil
	default:
		return fmt.Errorf("unknown format %q (want human|json)", *format)
	}
}
