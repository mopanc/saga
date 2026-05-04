package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runSetupClaude(args []string) error {
	fs := flag.NewFlagSet("setup-claude", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga setup-claude — print the JSON snippet to add to ~/.claude/settings.json so Claude Code uses saga as MCP server + UserPromptSubmit hook.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !filepath.IsAbs(exe) {
		exe, _ = filepath.Abs(exe)
	}

	if strings.ContainsAny(exe, " \t") {
		fmt.Fprintf(os.Stderr, "Warning: binary path contains whitespace; manual quoting may be required: %q\n\n", exe)
	}

	home, _ := os.UserHomeDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	snippet := fmt.Sprintf(`{
  "mcpServers": {
    "saga": {
      "command": %q,
      "args": ["mcp"]
    }
  },
  "hooks": {
    "UserPromptSubmit": [{
      "hooks": [{
        "type": "command",
        "command": %q
      }]
    }]
  }
}
`, exe, exe+" hook")

	fmt.Println("# Wire Saga into Claude Code")
	fmt.Println()
	fmt.Printf("Merge the following into %s\n", settingsPath)
	fmt.Println("(if the file already has 'mcpServers' or 'hooks', combine the keys — don't overwrite):")
	fmt.Println()
	fmt.Print(snippet)

	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		fmt.Println()
		fmt.Printf("Note: %s does not exist yet. Create the file with the snippet above as its full contents.\n", settingsPath)
	}
	return nil
}
