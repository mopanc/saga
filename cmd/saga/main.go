// saga — single-binary entrypoint with subcommands.
//
// All Saga functionality (CLI, MCP server, Claude hook, project init,
// settings.json wiring) lives behind one binary. Distribution is one file;
// users install with `go install ./cmd/saga` and that's it.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mopanc/saga/internal/saga"
)

const usage = `saga v%s — AI investigation memory

Usage:
  saga <command> [options]

Commands:
  version          Print version
  init             Initialise .saga/ in the current project
  reindex          Rebuild SQLite index from markdown in active layers
  sync             Pull/push the personal layer between machines (auto-commit + rebase)
  lembrancas       List recent recall events from the index
  gc               Report (and optionally reclaim) history of topics not in the index
  rules            List the policy notes in force for the current directory
  vault            Assemble every layer into one directory for Obsidian
  conflicts        List @conflicts_with topic pairs in active layers
  show             Display a topic plus its incoming and outgoing relations
  capabilities     Print engine capability declaration (spec/types/operators)
  lint             Validate topics against Saga Topic Spec v1.0
  doctor           Diagnose installation, config, and content state
  mcp              Run MCP stdio server (invoked by AI clients)
  hook             Run UserPromptSubmit hook (invoked by Claude Code)
  guard            Run PreToolUse hook — inject rules that govern the pending action
  setup-claude     Print Claude Code config snippet to wire saga in

Run 'saga help <command>' for command-specific notes.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, saga.VersionString())
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("saga v%s\n", saga.VersionString())
		return
	case "help", "-h", "--help":
		// `saga help <command>` is the form the general usage text tells the
		// user to run, so it has to work. Per-command help lives in each
		// command's own flag set, so hand the request there rather than
		// maintaining a second copy of every command's documentation.
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			cmd, args = args[0], []string{"--help"}
			break
		}
		fmt.Fprintf(os.Stdout, usage, saga.VersionString())
		return
	}

	if err := dispatch(cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "saga: %v\n", err)
		os.Exit(1)
	}
}

// dispatch routes a command name to its handler. Separate from main so that
// `saga help <command>` can re-enter it after rewriting the arguments.
func dispatch(cmd string, args []string) error {
	switch cmd {
	case "init":
		return runInit(args)
	case "reindex":
		return runReindex(args)
	case "sync":
		return runSync(args)
	case "lembrancas":
		return runLembrancas(args)
	case "gc":
		return runGC(args)
	case "rules":
		return runRules(args)
	case "vault":
		return runVault(args)
	case "conflicts":
		return runConflicts(args)
	case "show":
		return runShow(args)
	case "capabilities":
		return runCapabilities(args)
	case "lint":
		return runLint(args)
	case "doctor":
		return runDoctor(args)
	case "mcp":
		return runMCP(args)
	case "hook":
		return runHook(args)
	case "guard":
		return runGuard(args) // fail-silent internally; always returns nil
	case "setup-claude":
		return runSetupClaude(args)
	}
	fmt.Fprintf(os.Stderr, "saga: unknown command %q\n\n", cmd)
	fmt.Fprintf(os.Stderr, usage, saga.VersionString())
	os.Exit(2)
	return nil
}
