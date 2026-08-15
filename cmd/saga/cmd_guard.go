package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mopanc/saga/internal/saga"
)

// maxGuardContextChars bounds the injected rule text. Claude Code caps
// additionalContext at 10k chars; stay well under so a long rule cannot cost
// the caller its own budget.
const maxGuardContextChars = 6000

// preToolUseEvent is the subset of the PreToolUse payload saga reads. Unknown
// fields are ignored, so newer hosts stay compatible.
type preToolUseEvent struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	Cwd       string          `json:"cwd"`
}

// guardOutput is the PreToolUse hook response contract.
type guardOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		AdditionalContext        string `json:"additionalContext,omitempty"`
		PermissionDecision       string `json:"permissionDecision,omitempty"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	} `json:"hookSpecificOutput"`
}

// runGuard is the PreToolUse hook: it injects the rules governing the action
// about to happen, at the moment it happens.
//
// This is the deterministic half of rule delivery. The always-on catalogue
// (<saga-rules>) makes every rule discoverable but still relies on the agent
// noticing an entry applies; the guard fires whether or not attention lands on
// it, and costs nothing when no rule matches.
//
// Never blocks on internal failure: a saga fault must not stop the user's work.
// The one exception is a rule that explicitly declares `enforcement: block`,
// where refusing IS the intended behaviour.
func runGuard(args []string) error {
	fs := flag.NewFlagSet("guard", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `saga guard — Claude Code PreToolUse hook. Reads the tool call on stdin and
injects the policy notes whose `+"`triggers`"+` match it. Rules with
`+"`enforcement: block`"+` deny the call instead. Not normally run by hand.`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := runGuardInner(); err != nil {
		fmt.Fprintf(os.Stderr, "saga guard: %v\n", err)
	}
	return nil
}

func runGuardInner() error {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var event preToolUseEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("parse event: %w", err)
	}
	if event.ToolName == "" {
		return nil
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

	cwd := event.Cwd
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	svc := saga.NewService(db, cfg, cwd)
	rules, err := svc.RulesForAction(event.ToolName, actionArgument(event.ToolInput))
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}

	// RulesForAction returns blocking rules first, so the first match decides.
	if rules[0].Enforcement == saga.EnforcementBlock {
		emitGuard(os.Stdout, "", "deny", fmt.Sprintf(
			"Blocked by the rule %q (%s). Read it in full with `saga show %s`, "+
				"then either comply or ask the user to relax the rule. Do not work around it.",
			rules[0].Title, rules[0].Scope, rules[0].ID))
		logGuardUse(svc, rules)
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Rules that govern this action. The user wrote them and expects them followed.\n\n")
	for _, r := range rules {
		fmt.Fprintf(&sb, "## %s `[%s]`\n", r.Title, r.Scope)
		body, err := readBody(r.FilePath)
		if err == nil && body != "" {
			sb.WriteString(truncateTopicBody(body, maxGuardContextChars/len(rules)))
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "(full note: saga show %s)\n\n", r.ID)
	}

	emitGuard(os.Stdout, sb.String(), "", "")
	logGuardUse(svc, rules)
	return nil
}

// actionArgument reduces a tool input object to the single string a trigger
// glob is matched against. Bash-style tools carry a command; file tools carry a
// path. Anything else yields an empty argument, so only bare-name triggers
// (e.g. "WebFetch") can match it — deliberately conservative: inventing an
// argument would make matching unpredictable across hosts.
func actionArgument(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var fields struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(input, &fields); err != nil {
		return ""
	}
	for _, v := range []string{fields.Command, fields.FilePath, fields.Path, fields.URL} {
		if v != "" {
			return v
		}
	}
	return ""
}

func emitGuard(w io.Writer, context, decision, reason string) {
	var out guardOutput
	out.HookSpecificOutput.HookEventName = "PreToolUse"
	out.HookSpecificOutput.AdditionalContext = context
	out.HookSpecificOutput.PermissionDecision = decision
	out.HookSpecificOutput.PermissionDecisionReason = reason
	enc := json.NewEncoder(w)
	_ = enc.Encode(out)
}

// logGuardUse records that these rules were brought to the conversation, so a
// rule that fires often is visible in the usage history like any other recall.
func logGuardUse(svc *saga.Service, rules []saga.TriggeredRule) {
	ids := make([]string, len(rules))
	for i, r := range rules {
		ids[i] = r.ID
	}
	_ = svc.LogLembrancas(ids, saga.LembrancaKindHook, "pre-tool-use")
}
