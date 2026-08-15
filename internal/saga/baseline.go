package saga

import (
	"fmt"
	"os"
	"strings"
)

// DefaultBaselineMaxTokens caps the size of the always-on identity block
// the hook prepends to every prompt. Configurable via SAGA_BASELINE_MAX_TOKENS;
// the default is conservative — large enough to convey identity + style,
// small enough to leave headroom for the user's actual prompt.
const DefaultBaselineMaxTokens = 400

// BuildIdentityBaseline assembles a compact markdown block summarising the
// user's identity from personal-layer profile and preference notes. Returns:
//   - the rendered baseline (empty string if no profile/preference notes)
//   - the IDs of notes that contributed to the baseline (for lembrança logging)
//   - error
//
// Empty result is not an error — callers treat as "no baseline available".
// Iteration F populates the notes; until then this gracefully returns "" and
// the hook degrades to topic-only injection.
//
// Token estimation is intentionally simple (~4 chars per token). Every note
// gets a share of the budget and is cut at a paragraph boundary, never
// mid-sentence; no note is dropped for being late in the ordering. Even when
// content is truncated, the IDs of all considered notes are returned — they
// all "contributed", and we want their lembrança records.
func (s *Service) BuildIdentityBaseline(maxTokens int) (string, []string, error) {
	if maxTokens <= 0 {
		maxTokens = DefaultBaselineMaxTokens
	}

	notes, err := s.notesByScopeAndType("personal", []string{"profile", "preference"})
	if err != nil {
		return "", nil, fmt.Errorf("query personal notes: %w", err)
	}
	if len(notes) == 0 {
		return "", nil, nil
	}

	var profiles, preferences []*Topic
	for _, t := range notes {
		switch t.Type {
		case "profile":
			profiles = append(profiles, t)
		case "preference":
			preferences = append(preferences, t)
		}
	}

	// Allocate the budget per note rather than truncating the concatenation.
	// Global truncation is first-come-first-served: with 9 notes totalling
	// ~17.9k chars against a 400-token (~1.6k char) cap, the first profile note
	// consumed the entire budget and every `preference` note was silently
	// dropped — the user's stated preferences never reached the model at all.
	// Fair shares mean every note is represented, each cut at a paragraph
	// boundary.
	//
	// The bodies are budgeted against what is left after the scaffolding. The
	// same bug bites one level down otherwise: allocate the full budget to
	// bodies, render, then character-cap the result, and the headers push the
	// total over so the cap falls on the tail note.
	ordered := append(append([]*Topic{}, profiles...), preferences...)
	overhead := 0
	for _, t := range ordered {
		overhead += len("## ") + len(t.Title) + len("\n") + len("\n\n")
	}
	if len(profiles) > 0 {
		overhead += len("# Profile\n\n")
	}
	if len(preferences) > 0 {
		overhead += len("# Preferences\n\n")
	}
	shares := allocateShares(ordered, maxTokens*4-overhead)

	var sb strings.Builder
	if len(profiles) > 0 {
		sb.WriteString("# Profile\n\n")
		for _, t := range profiles {
			fmt.Fprintf(&sb, "## %s\n%s\n\n", t.Title, truncateAtBoundary(t.Body, shares[t.ID]))
		}
	}
	if len(preferences) > 0 {
		sb.WriteString("# Preferences\n\n")
		for _, t := range preferences {
			fmt.Fprintf(&sb, "## %s\n%s\n\n", t.Title, truncateAtBoundary(t.Body, shares[t.ID]))
		}
	}

	usedIDs := make([]string, 0, len(profiles)+len(preferences))
	for _, t := range profiles {
		usedIDs = append(usedIDs, t.ID)
	}
	for _, t := range preferences {
		usedIDs = append(usedIDs, t.ID)
	}

	// No global character cap here. The per-note allocation above already bounds
	// the result, and a trailing character cut is precisely what dropped whole
	// notes off the end before.
	return strings.TrimRight(sb.String(), "\n"), usedIDs, nil
}

// allocateShares divides a character budget across notes, giving every note a
// slice so none is dropped entirely. Notes shorter than an equal share release
// their surplus to the rest, so a budget is never wasted on a one-line note
// while a long one is cut to the bone.
//
// Returns a map from topic id to that note's character allowance.
func allocateShares(notes []*Topic, budget int) map[string]int {
	shares := make(map[string]int, len(notes))
	if len(notes) == 0 || budget <= 0 {
		return shares
	}

	remaining := budget
	unsatisfied := make([]*Topic, 0, len(notes))
	unsatisfied = append(unsatisfied, notes...)

	// Repeatedly hand out equal shares; notes that need less than their share
	// take only what they need and free the rest for the next round.
	for len(unsatisfied) > 0 {
		share := remaining / len(unsatisfied)
		if share <= 0 {
			break
		}
		next := unsatisfied[:0:0]
		progressed := false
		for _, t := range unsatisfied {
			if len(t.Body) <= share {
				shares[t.ID] = len(t.Body)
				remaining -= len(t.Body)
				progressed = true
				continue
			}
			next = append(next, t)
		}
		if !progressed {
			// Everyone still wants more than an equal share: split evenly.
			for _, t := range next {
				shares[t.ID] = share
			}
			break
		}
		unsatisfied = next
	}
	return shares
}

// truncateAtBoundary cuts text to at most maxChars, preferring a paragraph
// break, then a line break, so output never stops mid-sentence. An elision
// marker is appended when content was dropped, so the model can tell the
// difference between a short note and a trimmed one and knows to read the full
// note when it matters.
func truncateAtBoundary(text string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if len(text) <= maxChars {
		return text
	}
	cut := text[:maxChars]
	if idx := strings.LastIndex(cut, "\n\n"); idx > 0 {
		cut = text[:idx]
	} else if idx := strings.LastIndex(cut, "\n"); idx > 0 {
		cut = text[:idx]
	}
	return strings.TrimRight(cut, "\n") + "\n[…]"
}

// notesByScopeAndType returns parsed Topic structs for the given scope and
// type filter, ordered deterministically (by type, then title) so the
// baseline output is stable across invocations.
//
// Files referenced by the index but missing on disk are skipped silently —
// the index may be temporarily stale (e.g. user deleted a file before
// `saga reindex`). Files that exist but fail to parse are also skipped;
// the index will be cleaned on next reindex.
func (s *Service) notesByScopeAndType(scope string, types []string) ([]*Topic, error) {
	if len(types) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(types))
	placeholders = placeholders[:len(placeholders)-1]

	qArgs := make([]any, 0, len(types)+1)
	qArgs = append(qArgs, scope)
	for _, t := range types {
		qArgs = append(qArgs, t)
	}

	sqlStr := fmt.Sprintf(`
		SELECT file_path
		FROM topic_index
		WHERE scope = ? AND type IN (%s)
		ORDER BY type, title
	`, placeholders)

	rows, err := s.db.Query(sqlStr, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []*Topic
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path) // #nosec G304 -- path is from saga's own topic_index DB
		if err != nil {
			continue
		}
		topic, err := ParseTopic(content)
		if err != nil {
			continue
		}
		topics = append(topics, topic)
	}
	return topics, rows.Err()
}
