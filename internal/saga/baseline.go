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
// user's identity from personal-layer profile and preference notes. The
// result is what the hook injects on every prompt as <saga-identity>.
//
// Returns an empty string if no profile/preference notes exist — callers
// must treat this as "no baseline available", not as error. Iteration F
// of the roadmap is what fills these notes; until then, this function
// gracefully returns "" and the hook degrades to topic-only injection.
//
// Token estimation is intentionally simple (~4 chars per token). Truncation
// cuts at the nearest paragraph boundary above the limit, never mid-sentence.
func (s *Service) BuildIdentityBaseline(maxTokens int) (string, error) {
	if maxTokens <= 0 {
		maxTokens = DefaultBaselineMaxTokens
	}

	notes, err := s.notesByScopeAndType("personal", []string{"profile", "preference"})
	if err != nil {
		return "", fmt.Errorf("query personal notes: %w", err)
	}
	if len(notes) == 0 {
		return "", nil
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

	var sb strings.Builder
	if len(profiles) > 0 {
		sb.WriteString("# Profile\n\n")
		for _, t := range profiles {
			fmt.Fprintf(&sb, "## %s\n%s\n\n", t.Title, t.Body)
		}
	}
	if len(preferences) > 0 {
		sb.WriteString("# Preferences\n\n")
		for _, t := range preferences {
			fmt.Fprintf(&sb, "## %s\n%s\n\n", t.Title, t.Body)
		}
	}

	out := strings.TrimRight(sb.String(), "\n")
	return truncateAtSection(out, maxTokens), nil
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
		content, err := os.ReadFile(path)
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

// truncateAtSection trims `text` to fit within roughly maxTokens (4 chars ≈
// 1 token), cutting at the nearest paragraph boundary (\n\n) above the
// limit so output never stops mid-sentence. Falls back to a single
// newline boundary if no paragraph break exists; only as last resort
// returns a hard char-length cut.
func truncateAtSection(text string, maxTokens int) string {
	maxChars := maxTokens * 4
	if len(text) <= maxChars {
		return text
	}
	cut := text[:maxChars]
	if idx := strings.LastIndex(cut, "\n\n"); idx > 0 {
		return strings.TrimRight(text[:idx], "\n")
	}
	if idx := strings.LastIndex(cut, "\n"); idx > 0 {
		return strings.TrimRight(text[:idx], "\n")
	}
	return cut
}
