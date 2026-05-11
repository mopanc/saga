package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mopanc/saga/internal/saga"
)

const syncUsage = `saga sync — push and pull the personal layer between machines.

Usage:
  saga sync                    pull --rebase, then push (auto-commits local changes)
  saga sync --pull             only pull (skips auto-commit)
  saga sync --push             only push (auto-commits, no pull)
  saga sync --status           show local vs remote state without changing anything
  saga sync --dry-run          show what would be pushed and what is excluded, no mutation
  saga sync --no-auto-commit   skip auto-commit (require manual commit before push)

Topics with frontmatter sensitivity: confidential are never pushed — they stay
local-only on disk. Use --dry-run to preview the plan first.

Sync uses the git remote configured at ~/.saga/personal/.git. To bootstrap:
  cd ~/.saga/personal
  git init && git add -A && git commit -m 'init'
  git remote add origin <url>
  git push -u origin main
`

func runSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, syncUsage) }
	pullOnly := fs.Bool("pull", false, "only pull (skip push)")
	pushOnly := fs.Bool("push", false, "only push (skip pull)")
	statusOnly := fs.Bool("status", false, "show status without applying changes")
	dryRun := fs.Bool("dry-run", false, "show what would be pushed and what is excluded, without mutating")
	noAutoCommit := fs.Bool("no-auto-commit", false, "skip auto-commit of pending changes")
	commitMsg := fs.String("message", "", "auto-commit message override (default: \"saga: sync <RFC3339>\")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pullOnly && *pushOnly {
		return errors.New("--pull and --push are mutually exclusive")
	}
	if *dryRun && *statusOnly {
		return errors.New("--dry-run and --status are mutually exclusive")
	}

	cfg, err := saga.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	layerDir := saga.PersonalLayerDir(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *statusOnly {
		return printSyncStatus(ctx, layerDir)
	}

	res, err := saga.Sync(ctx, layerDir, saga.SyncOptions{
		PullOnly:     *pullOnly,
		PushOnly:     *pushOnly,
		NoAutoCommit: *noAutoCommit,
		DryRun:       *dryRun,
		CommitMsg:    *commitMsg,
	})
	if err != nil {
		if errors.Is(err, saga.ErrNoRemote) {
			fmt.Fprintln(os.Stderr, "saga sync: personal layer has no git remote.")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Bootstrap with:")
			fmt.Fprintf(os.Stderr, "  cd %s\n", layerDir)
			fmt.Fprintln(os.Stderr, "  git init && git add -A && git commit -m 'init'")
			fmt.Fprintln(os.Stderr, "  git remote add origin <url>")
			fmt.Fprintln(os.Stderr, "  git push -u origin main")
			return err
		}
		var conflict *saga.SyncConflictError
		if errors.As(err, &conflict) {
			fmt.Fprintln(os.Stderr, conflict.Error())
			return err
		}
		return err
	}

	if *dryRun {
		printSyncDryRun(res)
		return nil
	}

	printSyncResult(res)

	// Reindex after a successful pull so the SQLite index reflects pulled changes.
	if res.Pulled {
		if err := reindexAfterPull(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: reindex after pull failed: %v\n", err)
		}
	}
	return nil
}

func printSyncResult(r *saga.SyncResult) {
	fmt.Printf("saga sync: branch=%s remote=%s\n", r.Branch, r.Remote)
	switch {
	case r.Committed:
		fmt.Println("  ✓ committed local changes")
	case !r.Committed && !r.Pulled && !r.Pushed:
		// nothing to report beyond the header
	default:
		fmt.Println("  · no local changes to commit")
	}
	if r.Pulled {
		if r.PullOutput != "" {
			fmt.Println("  ✓ pulled (rebased)")
			indentPrint(r.PullOutput)
		} else {
			fmt.Println("  ✓ pulled — already up to date")
		}
	}
	if r.Pushed {
		if r.PushOutput != "" {
			fmt.Println("  ✓ pushed")
			indentPrint(r.PushOutput)
		} else {
			fmt.Println("  ✓ pushed — nothing new")
		}
	}
	if n := len(r.ExcludedConfidential); n > 0 {
		fmt.Printf("  · %d confidential topic(s) kept local-only\n", n)
	}
	printAlreadyPushedWarnings(r)
}

func printAlreadyPushedWarnings(r *saga.SyncResult) {
	if len(r.AlreadyPushedWarnings) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "warning: confidential topic(s) already exist in the remote — marking confidential locally")
	fmt.Fprintln(os.Stderr, "does NOT retroactively remove them. Each was published in an earlier sync:")
	for _, w := range r.AlreadyPushedWarnings {
		fmt.Fprintf(os.Stderr, "  %s  (%s)\n", w.FilePath, w.ID)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "To force-remove from the remote, use git directly until `saga sync --purge` lands:")
	fmt.Fprintln(os.Stderr, "  cd "+r.LayerDir)
	fmt.Fprintln(os.Stderr, "  git rm --cached <path> && git commit -m 'remove confidential' && git push")
}

func printSyncDryRun(r *saga.SyncResult) {
	fmt.Printf("saga sync --dry-run: branch=%s remote=%s\n", r.Branch, r.Remote)
	if len(r.PendingAdds) == 0 {
		fmt.Println("  · would push: nothing — working tree matches index")
	} else {
		fmt.Printf("  would push %d change(s):\n", len(r.PendingAdds))
		for _, p := range r.PendingAdds {
			fmt.Printf("    + %s\n", p)
		}
	}
	if n := len(r.ExcludedConfidential); n > 0 {
		fmt.Printf("  would exclude %d confidential topic(s) (sensitivity: confidential):\n", n)
		for _, e := range r.ExcludedConfidential {
			fmt.Printf("    - %s  (%s)\n", e.FilePath, e.ID)
		}
	}
	printAlreadyPushedWarnings(r)
}

func printSyncStatus(ctx context.Context, layerDir string) error {
	rep, err := saga.SyncStatus(ctx, layerDir)
	if err != nil {
		return err
	}
	fmt.Printf("personal layer: %s\n", rep.LayerDir)
	if !rep.HasRemote {
		fmt.Println("  ✗ no git remote configured (run `saga sync` for bootstrap instructions)")
		return nil
	}
	fmt.Printf("  remote: %s\n", rep.Remote)
	fmt.Printf("  branch: %s\n", rep.Branch)
	if len(rep.UncommittedFiles) > 0 {
		fmt.Printf("  uncommitted: %d file(s)\n", len(rep.UncommittedFiles))
		for _, f := range rep.UncommittedFiles {
			fmt.Printf("    %s\n", f)
		}
	} else {
		fmt.Println("  uncommitted: none")
	}
	switch {
	case rep.AheadBy == 0 && rep.BehindBy == 0:
		fmt.Println("  in sync with origin (cached; run `git fetch` for fresh state)")
	case rep.AheadBy > 0 && rep.BehindBy == 0:
		fmt.Printf("  ahead of origin by %d commit(s) — `saga sync --push` to publish\n", rep.AheadBy)
	case rep.AheadBy == 0 && rep.BehindBy > 0:
		fmt.Printf("  behind origin by %d commit(s) — `saga sync --pull` to fetch\n", rep.BehindBy)
	default:
		fmt.Printf("  diverged: %d ahead, %d behind — `saga sync` to rebase + push\n", rep.AheadBy, rep.BehindBy)
	}
	return nil
}

func indentPrint(s string) {
	for _, line := range splitLines(s) {
		if line != "" {
			fmt.Printf("    %s\n", line)
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func reindexAfterPull(cfg *saga.Config) error {
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
	for _, layer := range layers {
		if layer.Scope != "personal" {
			continue
		}
		if _, err := db.IndexLayer(layer); err != nil {
			return fmt.Errorf("index personal: %w", err)
		}
	}
	return nil
}
