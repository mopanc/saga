package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/mopanc/saga/internal/saga"
)

const lintUsage = `saga lint — validate topics against Saga Topic Spec v1.0.

Walks every topic in active layers and reports drift from the spec:
required fields, type vocabulary, trait enums, slug ↔ title coherence,
relation target resolution, supersedes / derived_from cycles, duplicate ids.

Usage:
  saga lint                       lint all active layers (personal + project)
  saga lint --scope personal      restrict to a single layer scope
  saga lint --format json         machine-readable output for tooling
  saga lint --fix                 auto-apply safe fixes (currently: insert
                                  missing recommended defaults)

Exit codes:
  0   no findings
  1   one or more rule violations or warnings
  2   one or more files failed to parse (hard error)
`

func runLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, lintUsage) }
	scope := fs.String("scope", "", "restrict to a single layer scope (e.g. personal)")
	format := fs.String("format", "human", "output format: human|json")
	fix := fs.Bool("fix", false, "apply safe auto-fixes (insert missing recommended defaults)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "human" && *format != "json" {
		return fmt.Errorf("unknown format %q (want human|json)", *format)
	}

	cfg, err := saga.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	resolver := saga.NewResolver(cfg)
	layers, err := resolver.Resolve(cwd)
	if err != nil {
		return fmt.Errorf("resolve layers: %w", err)
	}

	report, err := saga.Lint(layers, saga.LintOptions{Scope: *scope, Fix: *fix})
	if err != nil {
		return fmt.Errorf("lint: %w", err)
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	default:
		printLintHuman(report)
	}

	switch {
	case report.ParseErrors > 0:
		os.Exit(2)
	case report.HasFindings():
		os.Exit(1)
	}
	return nil
}

func printLintHuman(r *saga.LintReport) {
	if len(r.Diagnostics) == 0 {
		fmt.Printf("saga lint: %d file(s) walked — clean ✓\n", r.FilesWalked)
		if len(r.Fixed) > 0 {
			fmt.Printf("  fixed: %d file(s)\n", len(r.Fixed))
			for _, f := range r.Fixed {
				fmt.Printf("    %s\n", f.FilePath)
				for _, c := range f.Changes {
					fmt.Printf("      · %s\n", c)
				}
			}
		}
		return
	}

	errs, warns := 0, 0
	for _, d := range r.Diagnostics {
		switch d.Severity {
		case saga.SeverityError:
			errs++
		case saga.SeverityWarn:
			warns++
		}
	}

	fmt.Printf("saga lint: %d file(s) walked — %d error(s), %d warning(s)\n\n", r.FilesWalked, errs, warns)

	currentFile := ""
	for _, d := range r.Diagnostics {
		if d.FilePath != currentFile {
			fmt.Printf("%s\n", d.FilePath)
			currentFile = d.FilePath
		}
		marker := "ERROR"
		if d.Severity == saga.SeverityWarn {
			marker = "WARN "
		}
		field := ""
		if d.Field != "" {
			field = " [" + d.Field + "]"
		}
		suffix := ""
		if d.Fixable {
			suffix = "  (--fix can apply)"
		}
		fmt.Printf("  %s %s%s  %s%s\n", marker, d.Category, field, d.Message, suffix)
	}

	if len(r.Fixed) > 0 {
		fmt.Printf("\nfixed: %d file(s)\n", len(r.Fixed))
		for _, f := range r.Fixed {
			fmt.Printf("  %s\n", f.FilePath)
			for _, c := range f.Changes {
				fmt.Printf("    · %s\n", c)
			}
		}
	}

	if r.ParseErrors > 0 {
		fmt.Printf("\n%d file(s) failed to parse — exit code 2.\n", r.ParseErrors)
	}
}
