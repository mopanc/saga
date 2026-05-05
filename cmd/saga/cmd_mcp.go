package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mopanc/saga/internal/mcp"
	"github.com/mopanc/saga/internal/saga"
)

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga mcp — run as MCP stdio server. Invoked by AI clients (Claude Code, Cursor, etc); not normally run by hand.")
	}
	if err := fs.Parse(args); err != nil {
		return err
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

	cwd, _ := os.Getwd()
	svc := saga.NewService(db, cfg, cwd)

	server := mcp.New("saga", saga.Version, sagaTools, dispatchTool(svc))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(os.Stderr, "saga mcp v%s — db=%s cwd=%s\n", saga.Version, cfg.DBPath, cwd)
	return server.Serve(ctx, os.Stdin, os.Stdout)
}

var sagaTools = []mcp.Tool{
	{
		Name: "recall",
		Description: "Search Saga's notes for SPECIFIC topics or investigations across active " +
			"layers (personal + project). NOTE: the user's identity, preferences and policies " +
			"are ALREADY injected automatically as <saga-identity> on every prompt — do NOT " +
			"use recall to discover who the user is or how they like to work; that context is " +
			"already in your prompt. Use recall ONLY for specific topics (architectures, " +
			"debugging, investigations, design decisions) the conversation might need. Returns " +
			"top-k matching topics ranked by FTS5 relevance.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query":  { "type": "string", "description": "Search query (keywords or short phrase)." },
				"k":      { "type": "number", "description": "Maximum results (default 3, max 50).", "minimum": 1, "maximum": 50 },
				"scope":  { "type": "string", "description": "Optional scope filter (e.g. 'project:acme-platform' or 'personal')." },
				"type":   { "type": "string", "description": "Optional type filter (profile|preference|policy|topic)." }
			},
			"required": ["query"]
		}`),
	},
	{
		Name: "topic_read",
		Description: "Read a full topic note by name (slug or human title). Use AFTER recall to " +
			"get complete context (architecture, history, references, open questions).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name":  { "type": "string", "description": "Topic name (slug or human title)." },
				"scope": { "type": "string", "description": "Optional scope disambiguator." }
			},
			"required": ["name"]
		}`),
	},
	{
		Name:        "topic_list",
		Description: "List topic notes visible in the active scope context. Use to discover what's already documented before doing fresh investigation.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"scope": { "type": "string", "description": "Optional scope filter." },
				"type":  { "type": "string", "description": "Optional type filter." }
			}
		}`),
	},
	{
		Name: "topic_write",
		Description: "Save or update a Saga note. Default scope=personal (private to the user). " +
			"Use whenever you've done substantial investigation or learned something durable " +
			"that future conversations should not have to redo. Use type='topic' for " +
			"investigations / architecture / debugging conclusions; type='profile' for durable " +
			"facts about who the user is; type='preference' for how they like to be " +
			"communicated with; type='policy' for rules they want followed (commit style, " +
			"branching, code conventions). Profile and preference notes are injected " +
			"automatically into every prompt as <saga-identity>. Append-mode by default if " +
			"the named note exists; new content is added under a dated section.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name":      { "type": "string", "description": "Topic name (used as filename slug)." },
				"scope":     { "type": "string", "description": "Scope to write into. Default: personal." },
				"title":     { "type": "string", "description": "Human-readable title. Defaults to name." },
				"synonyms":  { "type": "array", "items": { "type": "string" }, "description": "Alternative phrasings for matching." },
				"body":      { "type": "string", "description": "Markdown body of the note." },
				"mode":      { "type": "string", "enum": ["create","append","replace"], "description": "Default: append if exists, else create." },
				"references": { "type": "array", "items": { "type": "object", "properties": { "path": { "type": "string" }, "lines": { "type": "string" }, "blame_hash": { "type": "string" } } }, "description": "File references for staleness tracking." },
				"type":      { "type": "string", "enum": ["topic","profile","preference","policy"], "description": "Default: topic." }
			},
			"required": ["name", "body"]
		}`),
	},
}

func dispatchTool(svc *saga.Service) mcp.Handler {
	return func(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error) {
		switch name {
		case "recall":
			var p saga.RecallArgs
			if err := json.Unmarshal(args, &p); err != nil {
				return mcp.ErrorResult("invalid arguments: " + err.Error()), nil
			}
			results, err := svc.Recall(p)
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return formatRecall(results), nil

		case "topic_read":
			var p saga.TopicReadArgs
			if err := json.Unmarshal(args, &p); err != nil {
				return mcp.ErrorResult("invalid arguments: " + err.Error()), nil
			}
			topic, err := svc.TopicRead(p)
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return mcp.TextResult(formatTopic(topic)), nil

		case "topic_list":
			var p saga.TopicListArgs
			if err := json.Unmarshal(args, &p); err != nil {
				return mcp.ErrorResult("invalid arguments: " + err.Error()), nil
			}
			results, err := svc.TopicList(p)
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return formatList(results), nil

		case "topic_write":
			var p saga.TopicWriteArgs
			if err := json.Unmarshal(args, &p); err != nil {
				return mcp.ErrorResult("invalid arguments: " + err.Error()), nil
			}
			res, err := svc.TopicWrite(p)
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return mcp.TextResult(fmt.Sprintf("%s topic %q in scope %q\n%s",
				res.Action, p.Name, res.Scope, res.Path)), nil

		default:
			return mcp.ErrorResult("unknown tool: " + name), nil
		}
	}
}

func formatRecall(results []saga.TopicSnippet) mcp.Result {
	if len(results) == 0 {
		return mcp.TextResult("No matching topics.")
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. [%s | %s] %s\n   score=%.3f confidence=%s\n   file=%s\n",
			i+1, r.Scope, r.Type, r.Title, r.Score, r.Confidence, r.FilePath)
		if len(r.Synonyms) > 0 {
			fmt.Fprintf(&b, "   synonyms: %s\n", strings.Join(r.Synonyms, ", "))
		}
	}
	return mcp.TextResult(b.String())
}

func formatTopic(t *saga.Topic) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n_scope=%s type=%s confidence=%s sensitivity=%s_\n\n",
		t.Title, t.Scope, t.Type, t.Confidence, t.Sensitivity)
	if len(t.Synonyms) > 0 {
		fmt.Fprintf(&b, "synonyms: %s\n\n", strings.Join(t.Synonyms, ", "))
	}
	if len(t.References) > 0 {
		b.WriteString("References:\n")
		for _, r := range t.References {
			fmt.Fprintf(&b, "  - %s", r.Path)
			if r.Lines != "" {
				fmt.Fprintf(&b, ":%s", r.Lines)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(t.Body)
	return b.String()
}

func formatList(results []saga.TopicSummary) mcp.Result {
	if len(results) == 0 {
		return mcp.TextResult("No topics in active layers.")
	}
	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "- [%s | %s] %s\n", r.Scope, r.Type, r.Title)
	}
	return mcp.TextResult(b.String())
}
