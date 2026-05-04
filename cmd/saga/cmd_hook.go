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
	results, err := svc.Recall(saga.RecallArgs{Query: event.Prompt, K: hookTopK})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}
	emitContext(os.Stdout, results)
	return nil
}

func emitContext(w io.Writer, results []saga.TopicSnippet) {
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
