package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runSetupClaude(args []string) error {
	fs := flag.NewFlagSet("setup-claude", flag.ExitOnError)
	apply := fs.Bool("apply", false, "Run `claude mcp add` automatically (requires the claude CLI on PATH).")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga setup-claude — print the commands/snippets needed to wire saga into Claude Code as MCP server + UserPromptSubmit hook. Use --apply to run `claude mcp add` for you.")
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

	hookSnippet := fmt.Sprintf(`{
  "hooks": {
    "UserPromptSubmit": [{
      "hooks": [{
        "type": "command",
        "command": %q
      }]
    }]
  }
}
`, exe+" hook")

	mcpCmd := fmt.Sprintf("claude mcp add saga -s user -- %s mcp", exe)

	fmt.Println("# Wire Saga into Claude Code")
	fmt.Println()
	fmt.Println("Saga has two integration points and they live in different files —")
	fmt.Println("Claude Code reads MCP servers from ~/.claude.json (managed via the `claude` CLI),")
	fmt.Println("but UserPromptSubmit hooks from ~/.claude/settings.json. You need both.")
	fmt.Println()
	fmt.Println("## 1. Register the MCP server (so mcp__saga__* tools are available)")
	fmt.Println()
	fmt.Println("Run:")
	fmt.Println()
	fmt.Println("  " + mcpCmd)
	fmt.Println()
	fmt.Printf("## 2. Install the UserPromptSubmit hook (so saga injects context every prompt)\n\n")
	fmt.Printf("Merge into %s\n", settingsPath)
	fmt.Println("(if the file already has 'hooks', combine — don't overwrite):")
	fmt.Println()
	fmt.Print(hookSnippet)

	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		fmt.Println()
		fmt.Printf("Note: %s does not exist yet. Create it with the snippet above as its full contents.\n", settingsPath)
	}

	fmt.Println()
	fmt.Println("After installing both, restart any open Claude Code session — MCP servers are loaded at startup.")

	if *apply {
		fmt.Println()
		fmt.Println("## --apply: registering MCP server now")
		if err := applyMCPRegistration(exe); err != nil {
			fmt.Fprintf(os.Stderr, "saga setup-claude: %v\n", err)
			fmt.Fprintln(os.Stderr, "Run the command above manually.")
			return err
		}
		fmt.Println()
		fmt.Println("## --apply: merging UserPromptSubmit hook into settings.json")
		if err := applyHookRegistration(exe, settingsPath); err != nil {
			fmt.Fprintf(os.Stderr, "saga setup-claude: %v\n", err)
			fmt.Fprintln(os.Stderr, "Merge the hook snippet above manually.")
			return err
		}
	}

	return nil
}

// applyHookRegistration adds saga's UserPromptSubmit hook to
// ~/.claude/settings.json, preserving any other MCPs, hook events, or
// hook entries already present. Idempotent: if a UserPromptSubmit entry
// matching the saga binary is already wired, no-op.
//
// Safety:
//   - Refuses to touch a file that is not valid JSON (no auto-repair).
//   - Writes the previous content to settings.json.bak before mutating.
//   - Atomic rename via .tmp; never leaves the file half-written.
func applyHookRegistration(exe, settingsPath string) error {
	hookCmd := exe + " hook"

	var existing []byte
	data, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is the user's own ~/.claude/settings.json, derived from os.UserHomeDir()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", settingsPath, err)
	}
	existing = data

	cfg := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return fmt.Errorf("%s is not valid JSON; refusing to modify (fix the file or merge the snippet manually): %w", settingsPath, err)
		}
	}

	if hookAlreadyWired(cfg, "saga", "hook") {
		fmt.Println("(UserPromptSubmit hook already wired to saga — leaving as-is)")
		return nil
	}

	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	userPrompts, _ := hooks["UserPromptSubmit"].([]any)

	newGroup := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCmd,
			},
		},
	}
	userPrompts = append(userPrompts, newGroup)
	hooks["UserPromptSubmit"] = userPrompts
	cfg["hooks"] = hooks

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(settingsPath), err)
	}

	if len(existing) > 0 {
		bak := settingsPath + ".bak"
		if err := os.WriteFile(bak, existing, 0o600); err != nil { // #nosec G703 -- bak is settingsPath+".bak"; settingsPath is the user's own home-derived path
			return fmt.Errorf("write backup %s: %w", bak, err)
		}
		fmt.Printf("(backup written: %s)\n", bak)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	out = append(out, '\n')

	tmp := settingsPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, settingsPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Printf("UserPromptSubmit hook merged into %s.\n", settingsPath)
	return nil
}

// hookAlreadyWired reports whether any UserPromptSubmit hook entry has a
// command containing all of the given substrings. Used to make
// applyHookRegistration idempotent without coupling to the exact binary path
// (the user may have installed saga from a different location).
func hookAlreadyWired(cfg map[string]any, mustContain ...string) bool {
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		return false
	}
	userPrompts, ok := hooks["UserPromptSubmit"].([]any)
	if !ok {
		return false
	}
	for _, group := range userPrompts {
		gm, ok := group.(map[string]any)
		if !ok {
			continue
		}
		hs, ok := gm["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range hs {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			match := true
			for _, s := range mustContain {
				if !strings.Contains(cmd, s) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// applyMCPRegistration runs `claude mcp add saga -s user -- <exe> mcp` for
// the user. Treats "already exists" exit codes as success — re-running
// setup-claude --apply should be idempotent.
func applyMCPRegistration(exe string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found on PATH; cannot --apply (run the command manually)")
	}

	cmd := exec.Command("claude", "mcp", "add", "saga", "-s", "user", "--", exe, "mcp") // #nosec G204 -- claude is a fixed binary; exe is the path of the running saga binary
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(strings.ToLower(output), "already") {
			fmt.Println("(saga MCP server already registered — leaving as-is)")
			return nil
		}
		return fmt.Errorf("`claude mcp add` failed: %s", output)
	}
	if output != "" {
		fmt.Println(output)
	}
	return nil
}
