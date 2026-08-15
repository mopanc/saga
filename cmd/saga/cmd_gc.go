package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mopanc/saga/internal/saga"
)

func runGC(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ExitOnError)
	prune := fs.String("prune", "", "comma-separated topic ids whose orphaned history to delete (irreversible)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `saga gc — report lembrança history whose topic is not in the index.

  saga gc                      list orphaned history, newest activity first
  saga gc --prune <id>[,<id>]  delete the history of those topic ids

Since v1.0.0-rc.5 lembranças survive their topic leaving the index, so that
moving a note between layers keeps its usage history. The cost is that
genuinely deleted notes leave history behind; this command reclaims it.

An orphan is NOT necessarily garbage. A note filed in a project layer that is
not active in the current directory looks exactly like a deleted one from here.
Run this from a directory where the layers you care about are active, and pass
ids explicitly rather than pruning blind.`)
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

	if *prune != "" {
		var ids []string
		for _, id := range strings.Split(*prune, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return fmt.Errorf("--prune: no topic ids given")
		}
		n, err := db.PruneOrphanedLembrancas(ids)
		if err != nil {
			return err
		}
		fmt.Printf("deleted %d lembrança row(s) across %d topic id(s)\n", n, len(ids))
		return nil
	}

	orphans, err := db.OrphanedLembrancas()
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		fmt.Println("no orphaned lembranças — every topic with history is in the index")
		return nil
	}

	total := 0
	for _, o := range orphans {
		total += o.Count
	}
	fmt.Printf("%d orphaned lembrança row(s) across %d topic id(s):\n\n", total, len(orphans))
	for _, o := range orphans {
		fmt.Printf("  %-28s %5d row(s)   last %s\n",
			o.TopicID, o.Count, time.UnixMilli(o.LastTrigger).Format("2006-01-02"))
	}
	fmt.Println("\nA topic filed in a layer that is not active here looks orphaned from here.")
	fmt.Println("Verify before deleting:  saga gc --prune <id>")
	return nil
}
