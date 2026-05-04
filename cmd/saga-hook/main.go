// saga-hook — Claude Code UserPromptSubmit hook.
//
// Reads the prompt event JSON from stdin, queries Saga for relevant topics
// across active layers (personal + auto-discovered project), and emits a
// <saga-context> block on stdout that Claude Code prepends to the prompt.
//
// Register in ~/.claude/settings.json:
//
//	"hooks": {
//	  "UserPromptSubmit": [{
//	    "hooks": [{ "type": "command", "command": "/abs/path/to/saga-hook" }]
//	  }]
//	}
//
// Failure policy: never block the prompt. Errors are logged to stderr; the
// process exits 0 with no stdout output.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jorgemorais/saga/internal/saga"
)

const TopK = 3

type promptEvent struct {
	Prompt string `json:"prompt"`
	Cwd    string `json:"cwd"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "saga-hook: %v\n", err)
		// exit 0 — fail-silent
	}
}

func run() error {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	var event promptEvent
	_ = json.Unmarshal(raw, &event) // best-effort; fields default to ""

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
	results, err := svc.Recall(saga.RecallArgs{Query: event.Prompt, K: TopK})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}

	emit(os.Stdout, results)
	return nil
}

func emit(w io.Writer, results []saga.TopicSnippet) {
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
