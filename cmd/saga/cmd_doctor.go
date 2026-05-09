package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mopanc/saga/internal/saga"
)

const (
	statusOK   = "✓"
	statusWarn = "⚠"
	statusFail = "✗"
)

// check is a single diagnostic finding. fix is empty when status==OK.
type check struct {
	status string
	label  string
	detail string
	fix    string
}

// runDoctor produces a human-readable diagnostic report covering installation,
// home directory, Claude Code wiring, and content (Iter F progress).
//
// Always exits 0 — failures are reported, not raised. The intent is to leave
// the user with copy-pasteable fix commands for whatever's broken.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga doctor — diagnose installation, configuration, and content state")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, _ := saga.LoadConfig()

	fmt.Println("saga doctor — diagnostic report")
	fmt.Println()

	for _, section := range []struct {
		name   string
		checks []check
	}{
		{"Binary", checkBinary()},
		{"Saga home", checkSagaHome(cfg)},
		{"Claude Code wiring", checkClaudeWiring()},
		{"Content", checkContent(cfg)},
	} {
		fmt.Println(section.name)
		for _, c := range section.checks {
			printCheck(c)
		}
		fmt.Println()
	}
	return nil
}

func printCheck(c check) {
	fmt.Printf("  %s %s", c.status, c.label)
	if c.detail != "" {
		fmt.Printf(" — %s", c.detail)
	}
	fmt.Println()
	if c.fix != "" {
		for _, line := range strings.Split(strings.TrimRight(c.fix, "\n"), "\n") {
			fmt.Printf("      %s\n", line)
		}
	}
}

func checkBinary() []check {
	var results []check
	exe, err := os.Executable()
	if err != nil {
		return []check{{status: statusFail, label: "binary location unknown", detail: err.Error()}}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	results = append(results, check{
		status: statusOK,
		label:  "binary",
		detail: fmt.Sprintf("%s (v%s)", exe, saga.VersionString()),
	})

	pathSaga, err := exec.LookPath("saga")
	switch {
	case err != nil:
		results = append(results, check{
			status: statusFail,
			label:  "saga not in PATH",
			fix: "add the binary's directory to PATH:\n  export PATH=\"" + filepath.Dir(exe) +
				":$PATH\"\n# put the export above in ~/.zshrc (or shell rc) for permanence; then `source ~/.zshrc`",
		})
	case pathSaga != exe:
		if resolved, err := filepath.EvalSymlinks(pathSaga); err == nil {
			pathSaga = resolved
		}
		if pathSaga != exe {
			results = append(results, check{
				status: statusWarn,
				label:  fmt.Sprintf("PATH resolves to a different saga: %s", pathSaga),
				fix:    "you may have multiple installations. `which -a saga` to see them all; reinstall with `go install ./cmd/saga` from the source.",
			})
		} else {
			results = append(results, check{status: statusOK, label: "in PATH"})
		}
	default:
		results = append(results, check{status: statusOK, label: "in PATH"})
	}
	return results
}

func checkSagaHome(cfg *saga.Config) []check {
	var results []check
	if _, err := os.Stat(cfg.HomeDir); err != nil {
		return []check{{
			status: statusWarn,
			label:  cfg.HomeDir + " does not exist yet",
			detail: "auto-created on first hook fire or saga command",
		}}
	}
	results = append(results, check{status: statusOK, label: cfg.HomeDir})

	personalMeta := filepath.Join(cfg.HomeDir, "personal", "meta.yml")
	if _, err := os.Stat(personalMeta); err != nil {
		results = append(results, check{
			status: statusWarn,
			label:  "personal layer not yet initialised",
			fix:    "run any saga command (e.g. `saga reindex`) — personal auto-creates on first use",
		})
	} else {
		results = append(results, check{status: statusOK, label: "personal/ layer present"})
	}

	if _, err := os.Stat(cfg.DBPath); err != nil {
		results = append(results, check{
			status: statusWarn,
			label:  "index.db not present",
			fix:    "run `saga reindex` to create it",
		})
		return results
	}

	db, err := saga.OpenDB(cfg.DBPath)
	if err != nil {
		return append(results, check{
			status: statusFail,
			label:  "cannot open index.db",
			detail: err.Error(),
			fix:    "delete and rebuild: `rm " + cfg.DBPath + " && saga reindex`",
		})
	}
	defer db.Close()

	var migCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&migCount); err != nil {
		return append(results, check{
			status: statusFail,
			label:  "schema check failed",
			detail: err.Error(),
		})
	}
	results = append(results, check{
		status: statusOK,
		label:  fmt.Sprintf("index.db OK — %d migration(s) applied", migCount),
	})
	return results
}

func checkClaudeWiring() []check {
	var results []check
	home, err := os.UserHomeDir()
	if err != nil {
		return []check{{status: statusFail, label: "cannot resolve user home", detail: err.Error()}}
	}

	// MCP server lives in ~/.claude.json (managed by `claude mcp add`).
	// The hook lives in ~/.claude/settings.json. Different files; both required.
	results = append(results, checkMCPRegistration(filepath.Join(home, ".claude.json"))...)
	results = append(results, checkHookRegistration(filepath.Join(home, ".claude", "settings.json"))...)
	return results
}

func checkMCPRegistration(claudeJSONPath string) []check {
	data, err := os.ReadFile(claudeJSONPath) // #nosec G304 -- claudeJSONPath is the user's own ~/.claude.json, derived from os.UserHomeDir()
	if err != nil {
		return []check{{
			status: statusFail,
			label:  claudeJSONPath + " not present",
			fix:    "install Claude Code, then run `saga setup-claude --apply` (or the printed `claude mcp add ...` command)",
		}}
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return []check{{
			status: statusFail,
			label:  ".claude.json is not valid JSON",
			detail: err.Error(),
		}}
	}
	if mcps, ok := cfg["mcpServers"].(map[string]any); ok {
		if _, found := mcps["saga"]; found {
			return []check{{status: statusOK, label: "saga MCP server registered (~/.claude.json)"}}
		}
	}
	return []check{{
		status: statusFail,
		label:  "saga MCP server not registered",
		fix:    "run `saga setup-claude --apply`, or manually:\n  claude mcp add saga -s user -- $(which saga) mcp",
	}}
}

func checkHookRegistration(settingsPath string) []check {
	data, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is the user's own ~/.claude/settings.json, derived from os.UserHomeDir()
	if err != nil {
		return []check{{
			status: statusWarn,
			label:  settingsPath + " not present",
			fix:    "run `saga setup-claude` and paste the hooks snippet into the file",
		}}
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return []check{{
			status: statusFail,
			label:  "settings.json is not valid JSON",
			detail: err.Error(),
		}}
	}

	hooksOK := false
	if hooks, ok := cfg["hooks"].(map[string]any); ok {
		if userPrompts, ok := hooks["UserPromptSubmit"].([]any); ok {
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
					if cmd, ok := hm["command"].(string); ok &&
						strings.Contains(cmd, "saga") && strings.Contains(cmd, "hook") {
						hooksOK = true
					}
				}
			}
		}
	}
	if hooksOK {
		return []check{{status: statusOK, label: "UserPromptSubmit hook wired (~/.claude/settings.json)"}}
	}
	return []check{{
		status: statusFail,
		label:  "UserPromptSubmit hook not wired",
		fix:    "run `saga setup-claude` and merge the hooks block into ~/.claude/settings.json",
	}}
}

func checkContent(cfg *saga.Config) []check {
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return []check{{
			status: statusWarn,
			label:  "no database yet — run `saga reindex` after init",
		}}
	}
	db, err := saga.OpenDB(cfg.DBPath)
	if err != nil {
		return []check{{status: statusFail, label: "cannot open db", detail: err.Error()}}
	}
	defer db.Close()

	var results []check
	rows, err := db.Query("SELECT type, COUNT(*) FROM topic_index GROUP BY type ORDER BY type")
	if err != nil {
		return []check{{status: statusFail, label: "topic_index query failed", detail: err.Error()}}
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	total := 0
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err == nil {
			counts[typ] = n
			total += n
		}
	}

	if total == 0 {
		results = append(results, check{
			status: statusWarn,
			label:  "no notes yet — Iteration F (feeding) not started",
			fix:    "ask Claude in any session to read your bio / CV / project docs and call topic_write to populate identity, preferences, policies, and topics",
		})
	} else {
		results = append(results, check{
			status: statusOK,
			label: fmt.Sprintf("%d notes — profile=%d preference=%d policy=%d topic=%d",
				total, counts["profile"], counts["preference"], counts["policy"], counts["topic"]),
		})
	}

	weekAgo := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	var recentLembrancas int
	_ = db.QueryRow("SELECT COUNT(*) FROM lembranca WHERE triggered_at >= ?", weekAgo).Scan(&recentLembrancas)
	results = append(results, check{
		status: statusOK,
		label:  fmt.Sprintf("lembranças (last 7d): %d", recentLembrancas),
	})
	return results
}
