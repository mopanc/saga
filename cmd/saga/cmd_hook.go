package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mopanc/saga/internal/saga"
)

const (
	hookTopK = 3

	// maxTopicBodyChars caps the per-topic body inlined in the hook output.
	// The model can call mcp__saga__topic_read to fetch the full body when
	// the snippet is enough to know the topic is relevant. Without this cap
	// a single ~13KB topic body blows past Claude Code's hook output limit
	// and the entire <saga-context> block gets truncated to a 2KB preview.
	maxTopicBodyChars = 1000

	// maxHookOutputBytes is a defensive total cap on hook stdout. Above this,
	// Claude Code persists the output to disk and only injects a 2KB preview
	// into the model's context — defeating the purpose of the hook. We cap
	// well below that limit.
	maxHookOutputBytes = 8 * 1024

	// truncationMarker is appended to bodies that exceed maxTopicBodyChars,
	// signalling the model that the full content is one tool call away.
	truncationMarker = "\n[truncated — call mcp__saga__topic_read for full body]"
)

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

	// Always-on rule catalogue: the index of `policy` notes in force for this
	// cwd. Separate budget from the identity baseline — rules and identity
	// should never compete for the same tokens, which is how policies ended up
	// invisible in the first place.
	rules, ruleIDs, err := svc.BuildRuleCatalog(cfg.RuleCatalogMaxTokens)
	if err != nil {
		fmt.Fprintf(os.Stderr, "saga hook: rules: %v\n", err)
		rules = ""
		ruleIDs = nil
	}

	// Topic-relevance recall (existing F3.b path).
	results, err := svc.Recall(saga.RecallArgs{Query: event.Prompt, K: hookTopK})
	if err != nil {
		return err
	}

	emitLensBlock(os.Stdout, cfg, noteCount, baseline, rules, results)

	// Log lembranças for both injection paths. Best-effort — failures
	// are silent; we never block the prompt on logging issues.
	if len(baselineIDs) > 0 {
		_ = svc.LogLembrancas(baselineIDs, saga.LembrancaKindBaseline, "")
	}
	if len(ruleIDs) > 0 {
		_ = svc.LogLembrancas(ruleIDs, saga.LembrancaKindBaseline, "")
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
//   - <saga-rules> when the active layers hold any `policy` note. Emitted
//     before <saga-context> so rules are in scope before recalled material.
//   - <saga-context> when there are query-matched topics.
//
// The whole block is assembled into a buffer first so we can enforce
// maxHookOutputBytes — a defensive total cap that keeps the hook below
// Claude Code's stdout truncation threshold. Per-topic bodies are also
// individually capped via truncateTopicBody so a single oversized topic
// can't starve the others.
func emitLensBlock(w io.Writer, cfg *saga.Config, noteCount int, baseline, rules string, results []saga.TopicSnippet) {
	var buf bytes.Buffer
	emitMetaBlock(&buf, cfg, noteCount)

	if baseline != "" {
		fmt.Fprintln(&buf, "<saga-identity>")
		fmt.Fprintln(&buf, baseline)
		fmt.Fprintln(&buf, "</saga-identity>")
	}

	if rules != "" {
		fmt.Fprintln(&buf, "<saga-rules>")
		fmt.Fprintln(&buf, rules)
		fmt.Fprintln(&buf, "</saga-rules>")
	}

	if len(results) > 0 {
		fmt.Fprintln(&buf, "<saga-context>")
		for _, r := range results {
			fmt.Fprintf(&buf, "<topic name=%q scope=%q confidence=%q file=%q",
				r.Title, r.Scope, r.Confidence, r.FilePath)
			if len(r.ConflictsWith) > 0 {
				// Surface @conflicts_with edges so the model knows this topic
				// has a contested counterpart in the corpus (S0-4).
				fmt.Fprintf(&buf, " conflicts-with=%q", strings.Join(r.ConflictsWith, ","))
			}
			fmt.Fprintln(&buf, ">")
			if len(r.Synonyms) > 0 {
				fmt.Fprintf(&buf, "synonyms: %s\n\n", strings.Join(r.Synonyms, ", "))
			}
			body, err := readBody(r.FilePath)
			if err == nil && body != "" {
				fmt.Fprintln(&buf, truncateTopicBody(body, maxTopicBodyChars))
			}
			fmt.Fprintln(&buf, "</topic>")
		}
		fmt.Fprintln(&buf, "</saga-context>")
	}

	out := capHookOutput(buf.Bytes(), maxHookOutputBytes)
	_, _ = w.Write(out)
}

// truncateTopicBody trims body to fit within maxChars. Cuts at a paragraph
// boundary when one exists above the limit (\n\n), otherwise at a line
// boundary (\n), otherwise hard-cut. A truncation marker is appended so the
// model knows the full body is reachable via mcp__saga__topic_read.
func truncateTopicBody(body string, maxChars int) string {
	if len(body) <= maxChars {
		return body
	}
	cut := saga.TrimToRuneBoundary(body[:maxChars])
	if idx := strings.LastIndex(cut, "\n\n"); idx > 0 {
		return strings.TrimRight(body[:idx], "\n") + truncationMarker
	}
	if idx := strings.LastIndex(cut, "\n"); idx > 0 {
		return strings.TrimRight(body[:idx], "\n") + truncationMarker
	}
	return cut + truncationMarker
}

// capHookOutput enforces a hard byte ceiling on the hook output. If the
// buffer is at or below the limit, it's returned untouched. Above the
// limit, we cut at the last newline below the cap and append a marker so
// the model knows context was dropped. This is a defensive guard — proper
// per-topic truncation should keep us comfortably below the cap in normal
// operation.
func capHookOutput(out []byte, maxBytes int) []byte {
	if len(out) <= maxBytes {
		return out
	}

	notice := []byte("<!-- saga: hook output capped at " + fmt.Sprint(maxBytes) + " bytes; some context omitted -->\n")

	// Reserve room for the notice and for closing whatever sections the cut
	// lands inside. Appending them after trimming to maxBytes is what pushed
	// the real output past the cap it announces.
	budget := maxBytes - len(notice) - maxClosingTagsBytes
	if budget < 0 {
		budget = 0
	}

	trimmed := out[:budget]
	if idx := bytes.LastIndexByte(trimmed, '\n'); idx > 0 {
		trimmed = trimmed[:idx+1]
	}
	trimmed = []byte(saga.TrimToRuneBoundary(string(trimmed)))

	// Build a fresh slice: appending to `trimmed` would write into the caller's
	// backing array, and closingTagsFor must read the trimmed content, not the
	// content plus whatever was just appended to it.
	out = make([]byte, 0, len(trimmed)+len(notice)+maxClosingTagsBytes)
	out = append(out, trimmed...)
	out = append(out, notice...)
	return append(out, closingTagsFor(trimmed)...)
}

// hookSections are the blocks emitLensBlock can open, in emission order.
var hookSections = []string{"saga-meta", "saga-identity", "saga-rules", "saga-context"}

// maxClosingTagsBytes bounds what closingTagsFor can append, so capHookOutput
// can reserve room for it up front.
var maxClosingTagsBytes = func() int {
	n := 0
	for _, s := range hookSections {
		n += len(s) + len("</>\n")
	}
	return n
}()

// closingTagsFor returns the closing tags for any section left open by a
// truncation. A cut through <saga-context> used to emit an opening tag with no
// closer, handing the client malformed markup on every capped prompt.
func closingTagsFor(out []byte) []byte {
	var closers []byte
	for _, name := range hookSections {
		open := bytes.Count(out, []byte("<"+name+">"))
		closed := bytes.Count(out, []byte("</"+name+">"))
		for i := 0; i < open-closed; i++ {
			closers = append(closers, []byte("</"+name+">\n")...)
		}
	}
	return closers
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
	fmt.Fprintf(w, "saga v%s wired in. db=%s, %s%s\n", saga.VersionString(), dbPath, countStr, state)
	fmt.Fprintln(w, "tools: mcp__saga__topic_write, mcp__saga__recall, mcp__saga__topic_read, mcp__saga__topic_list, mcp__saga__lembranca_log")
	fmt.Fprintln(w, "use topic_write (type=profile|preference|policy|topic, default scope=personal) to persist anything future sessions should not have to rediscover; use recall before doing investigation already covered by an existing note")
	fmt.Fprintln(w, "</saga-meta>")
}

func readBody(path string) (string, error) {
	content, err := os.ReadFile(path) // #nosec G304 -- path comes from TopicSnippet.FilePath, sourced from saga's own indexed topic_index DB
	if err != nil {
		return "", err
	}
	topic, err := saga.ParseTopic(content)
	if err != nil {
		return "", err
	}
	return topic.Body, nil
}
