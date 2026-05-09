// saga — single-binary entrypoint with subcommands.
//
// All Saga functionality (CLI, MCP server, Claude hook, project init,
// settings.json wiring) lives behind one binary. Distribution is one file;
// users install with `go install ./cmd/saga` and that's it.
package main

import (
	"fmt"
	"os"

	"github.com/mopanc/saga/internal/saga"
)

const usage = `saga v%s — AI investigation memory

Usage:
  saga <command> [options]

Commands:
  version          Print version
  init             Initialise .saga/ in the current project
  reindex          Rebuild SQLite index from markdown in active layers
  lembrancas       List recent recall events from the index
  doctor           Diagnose installation, config, and content state
  mcp              Run MCP stdio server (invoked by AI clients)
  hook             Run UserPromptSubmit hook (invoked by Claude Code)
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

	var err error
	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("saga v%s\n", saga.VersionString())
		return
	case "help", "-h", "--help":
		fmt.Fprintf(os.Stdout, usage, saga.VersionString())
		return
	case "init":
		err = runInit(args)
	case "reindex":
		err = runReindex(args)
	case "lembrancas":
		err = runLembrancas(args)
	case "doctor":
		err = runDoctor(args)
	case "mcp":
		err = runMCP(args)
	case "hook":
		err = runHook(args) // fail-silent internally; always returns nil
	case "setup-claude":
		err = runSetupClaude(args)
	default:
		fmt.Fprintf(os.Stderr, "saga: unknown command %q\n\n", cmd)
		fmt.Fprintf(os.Stderr, usage, saga.VersionString())
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "saga %s: %v\n", cmd, err)
		os.Exit(1)
	}
}
