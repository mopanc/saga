package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mopanc/saga/internal/saga"
)

func runLembrancas(args []string) error {
	fs := flag.NewFlagSet("lembrancas", flag.ExitOnError)
	since := fs.Duration("since", 24*time.Hour, "Only entries newer than this duration (e.g. 30m, 7d, 720h)")
	kind := fs.String("kind", "", "Filter by kind (hook|recall|topic_read|baseline)")
	topic := fs.String("topic", "", "Filter by topic title or topic id")
	limit := fs.Int("limit", 50, "Maximum entries (1-1000)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga lembrancas — list recent recall events from the index")
		fs.PrintDefaults()
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

	cwd, _ := os.Getwd()
	svc := saga.NewService(db, cfg, cwd)

	sinceMs := time.Now().Add(-*since).UnixMilli()
	entries, err := svc.LembrancaLog(saga.LembrancaQueryArgs{
		Since: sinceMs,
		Kind:  *kind,
		Topic: *topic,
		Limit: *limit,
	})
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No lembranças match the filter.")
		return nil
	}

	fmt.Printf("%d lembranças (most recent first):\n\n", len(entries))
	for _, e := range entries {
		ts := time.UnixMilli(e.TriggeredAt).Local().Format("2006-01-02 15:04:05")
		title := e.TopicTitle
		if title == "" {
			title = fmt.Sprintf("(deleted topic %s)", e.TopicID)
		}
		fmt.Printf("%s  %-10s  %s\n", ts, e.Kind, title)
		if e.Query != "" {
			fmt.Printf("    query: %q\n", e.Query)
		}
		if e.Cwd != "" {
			fmt.Printf("    cwd:   %s\n", e.Cwd)
		}
	}
	return nil
}
