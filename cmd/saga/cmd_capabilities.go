package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mopanc/saga/internal/saga"
)

// runCapabilities prints what this engine declares it offers.
// JSON output is machine-readable and intended for adopters / lint /
// future ecosystem tooling that wants to capability-negotiate.
func runCapabilities(args []string) error {
	fs := flag.NewFlagSet("capabilities", flag.ExitOnError)
	format := fs.String("format", "human", "output format: human|json")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga capabilities — declare what this engine offers per the saga-topic spec.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Adopters use this to capability-negotiate (spec §10): topics declare what")
		fmt.Fprintln(os.Stderr, "they need; engines declare what they offer; mismatches degrade gracefully.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	caps := saga.DescribeCapabilities()
	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(caps)
	case "human":
		fmt.Printf("saga engine v%s — saga-topic spec v%s, conformance level %d\n\n",
			caps.EngineVersion, caps.SpecVersion, caps.ConformanceLevel)

		fmt.Println("types implemented (specialised behaviour):")
		fmt.Printf("  %s\n\n", strings.Join(caps.TypesImplemented, ", "))

		fmt.Println("types accepted opaque (parsed and indexed; fall-through retrieval):")
		fmt.Printf("  %s\n\n", strings.Join(caps.TypesAcceptedOpaque, ", "))

		fmt.Println("pure-metadata operators (offered):")
		fmt.Printf("  %s\n\n", strings.Join(caps.OperatorsPureMeta, ", "))

		if len(caps.OperatorsRuntimeOff) == 0 {
			fmt.Println("runtime-required operators offered:")
			fmt.Println("  (none — specced but await cognition layer)")
			fmt.Println("")
		} else {
			fmt.Println("runtime-required operators offered:")
			fmt.Printf("  %s\n\n", strings.Join(caps.OperatorsRuntimeOff, ", "))
		}

		fmt.Println("runtime-required operators specced (not offered yet):")
		fmt.Printf("  %s\n\n", strings.Join(caps.OperatorsRuntimeSpec, ", "))

		fmt.Println("retrieval features:")
		fmt.Printf("  %s\n", strings.Join(caps.Retrieval, ", "))
		return nil
	default:
		return fmt.Errorf("unknown format %q (want human|json)", *format)
	}
}
