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

const hookTopK = 3

type hookEvent struct {
	Prompt string `json:"prompt"`
	Cwd    string `json:"cwd"`
}

// runHook is the Claude Code UserPromptSubmit hook. Always returns nil
// regardless of internal failure — never block the prompt on a hook fault.
// Errors are logged to stderr.
func runHook(args []string) error {
	fs := flag.NewFlagSet("hook", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga hook — Claude Code UserPromptSubmit hook. Invoked by Claude Code with event JSON on stdin; not normally run by hand.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := runHookInner(); err != nil {
		fmt.Fprintf(os.Stderr, "saga hook: %v\n", err)
	}
	return nil
}

func runHookInner() error {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	var event hookEvent
	_ = json.Unmarshal(raw, &event)

	if event.Prompt == "" {
		return nil
	}
	cwd := event.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	cfg, err := saga.LoadConfig()
	if err != nil {
		return err
	}
	db, err := saga.OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	svc := saga.NewService(db, cfg, cwd)

	noteCount, err := svc.CountTopics()
	if err != nil {
		fmt.Fprintf(os.Stderr, "saga hook: count: %v\n", err)
		noteCount = -1
	}

	// F3 — always-on lens: identity baseline emitted for every prompt,
	// independent of query. Empty when profile is empty (Iter F populates).
	baseline, baselineIDs, err := svc.BuildIdentityBaseline(cfg.BaselineMaxTokens)
	if err != nil {
		// Don't fail the hook on baseline error — prompt still flows through
		// without identity context.
		fmt.Fprintf(os.Stderr, "saga hook: baseline: %v\n", err)
		baseline = ""
		baselineIDs = nil
	}

	// Topic-relevance recall (existing F3.b path).
	results, err := svc.Recall(saga.RecallArgs{Query: event.Prompt, K: hookTopK})
	if err != nil {
		return err
	}

	emitLensBlock(os.Stdout, cfg, noteCount, baseline, results)

	// Log lembranças for both injection paths. Best-effort — failures
	// are silent; we never block the prompt on logging issues.
	if len(baselineIDs) > 0 {
		_ = svc.LogLembrancas(baselineIDs, saga.LembrancaKindBaseline, "")
	}
	if len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		_ = svc.LogLembrancas(ids, saga.LembrancaKindHook, event.Prompt)
	}

	return nil
}

// emitLensBlock writes the three-section context Claude Code prepends to the
// prompt:
//   - <saga-meta> always — bootstraps a fresh session even when saga is empty.
//     Tells the model the saga is wired in, what tools it exposes, and when
//     to call topic_write. Without this, an empty saga produces no signal at
//     all and the model has no way to discover that saga exists.
//   - <saga-identity> when there is a non-empty baseline (profile/preference
//     notes exist).
//   - <saga-context> when there are query-matched topics.
func emitLensBlock(w io.Writer, cfg *saga.Config, noteCount int, baseline string, results []saga.TopicSnippet) {
	emitMetaBlock(w, cfg, noteCount)

	if baseline != "" {
		fmt.Fprintln(w, "<saga-identity>")
		fmt.Fprintln(w, baseline)
		fmt.Fprintln(w, "</saga-identity>")
	}

	if len(results) == 0 {
		return
	}

	fmt.Fprintln(w, "<saga-context>")
	for _, r := range results {
		fmt.Fprintf(w, "<topic name=%q scope=%q confidence=%q file=%q>\n",
			r.Title, r.Scope, r.Confidence, r.FilePath)
		if len(r.Synonyms) > 0 {
			fmt.Fprintf(w, "synonyms: %s\n\n", strings.Join(r.Synonyms, ", "))
		}
		body, err := readBody(r.FilePath)
		if err == nil && body != "" {
			fmt.Fprintln(w, body)
		}
		fmt.Fprintln(w, "</topic>")
	}
	fmt.Fprintln(w, "</saga-context>")
}

// emitMetaBlock writes the bootstrap <saga-meta> block. Cheap (~80 tokens),
// emitted on every prompt so the model never has to guess whether saga is
// available. When the saga has notes, it still adds value — the count and
// tool list anchor the model's expectations.
func emitMetaBlock(w io.Writer, cfg *saga.Config, noteCount int) {
	dbPath := ""
	if cfg != nil {
		dbPath = cfg.DBPath
	}
	countStr := fmt.Sprintf("%d notes", noteCount)
	if noteCount < 0 {
		countStr = "note count unavailable"
	}
	state := ""
	if noteCount == 0 {
		state = " (empty — populate via topic_write when the user shares durable info about themselves, their projects, decisions, or preferences)"
	}

	fmt.Fprintln(w, "<saga-meta>")
	fmt.Fprintf(w, "saga v%s wired in. db=%s, %s%s\n", saga.Version, dbPath, countStr, state)
	fmt.Fprintln(w, "tools: mcp__saga__topic_write, mcp__saga__recall, mcp__saga__topic_read, mcp__saga__topic_list, mcp__saga__lembranca_log")
	fmt.Fprintln(w, "use topic_write (type=profile|preference|policy|topic, default scope=personal) to persist anything future sessions should not have to rediscover; use recall before doing investigation already covered by an existing note")
	fmt.Fprintln(w, "</saga-meta>")
}

func readBody(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	topic, err := saga.ParseTopic(content)
	if err != nil {
		return "", err
	}
	return topic.Body, nil
}
