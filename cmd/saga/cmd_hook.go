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

	// F3 — always-on lens: identity baseline emitted for every prompt,
	// independent of query. Empty when profile is empty (Iter F populates).
	baseline, err := svc.BuildIdentityBaseline(cfg.BaselineMaxTokens)
	if err != nil {
		// Don't fail the hook on baseline error — prompt still flows through
		// without identity context.
		fmt.Fprintf(os.Stderr, "saga hook: baseline: %v\n", err)
		baseline = ""
	}

	// Topic-relevance recall (existing F3.b path).
	results, err := svc.Recall(saga.RecallArgs{Query: event.Prompt, K: hookTopK})
	if err != nil {
		return err
	}

	emitLensBlock(os.Stdout, baseline, results)
	return nil
}

// emitLensBlock writes the two-section context Claude Code prepends to the
// prompt. <saga-identity> is emitted whenever there is a non-empty baseline;
// <saga-context> is emitted whenever there are query-matched topics. Either
// or both may be present; if both are absent, nothing is written and the
// prompt passes through unchanged.
func emitLensBlock(w io.Writer, baseline string, results []saga.TopicSnippet) {
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
