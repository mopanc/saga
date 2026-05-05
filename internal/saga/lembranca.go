package saga

import (
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Lembrança kinds — see COGNITIVE_MODEL.md §6 (episodic layer).
const (
	LembrancaKindHook      = "hook"       // injected via UserPromptSubmit hook (query-relevant match)
	LembrancaKindRecall    = "recall"     // returned by an explicit MCP recall call
	LembrancaKindTopicRead = "topic_read" // read by an explicit MCP topic_read call
	LembrancaKindBaseline  = "baseline"   // included in the always-on identity baseline
)

// LogLembrancas inserts one row per topic_id with shared kind/query/cwd and
// the same triggered_at (now). topicIDs may be empty (no-op).
//
// This is best-effort logging — callers commonly ignore the error since
// missing a log row should not break user-facing flows. Callers that
// require certainty can check the return.
func (db *DB) LogLembrancas(topicIDs []string, kind, query, cwd string) error {
	if len(topicIDs) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT INTO lembranca (id, topic_id, triggered_at, kind, query, cwd)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, tid := range topicIDs {
		if _, err := stmt.Exec(
			ulid.Make().String(), tid, now, kind,
			nullableString(query), nullableString(cwd),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert lembranca: %w", err)
		}
	}
	return tx.Commit()
}

// LogLembrancas — Service-level wrapper that uses the service's cwd.
func (s *Service) LogLembrancas(topicIDs []string, kind, query string) error {
	return s.db.LogLembrancas(topicIDs, kind, query, s.cwd)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// LembrancaQueryArgs filters for LembrancaLog inspection.
type LembrancaQueryArgs struct {
	Since int64  `json:"since,omitempty"` // unix ms, 0 = no lower bound
	Kind  string `json:"kind,omitempty"`  // hook|recall|topic_read|baseline
	Topic string `json:"topic,omitempty"` // topic title OR id
	Limit int    `json:"limit,omitempty"` // default 50, max 1000
}

// LembrancaEntry is a flat view of a single lembrança plus its topic title.
type LembrancaEntry struct {
	ID          string `json:"id"`
	TopicID     string `json:"topic_id"`
	TopicTitle  string `json:"topic_title"`
	TriggeredAt int64  `json:"triggered_at"`
	Kind        string `json:"kind"`
	Query       string `json:"query,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
}

// LembrancaLog returns lembrança events filtered by the given args, ordered
// by triggered_at DESC.
func (s *Service) LembrancaLog(args LembrancaQueryArgs) ([]LembrancaEntry, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	var conditions []string
	var qArgs []any
	if args.Since > 0 {
		conditions = append(conditions, "l.triggered_at >= ?")
		qArgs = append(qArgs, args.Since)
	}
	if args.Kind != "" {
		conditions = append(conditions, "l.kind = ?")
		qArgs = append(qArgs, args.Kind)
	}
	if args.Topic != "" {
		conditions = append(conditions, "(t.title = ? OR l.topic_id = ?)")
		qArgs = append(qArgs, args.Topic, args.Topic)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	qArgs = append(qArgs, limit)

	sqlStr := fmt.Sprintf(`
		SELECT l.id, l.topic_id, COALESCE(t.title, ''), l.triggered_at,
		       l.kind, COALESCE(l.query, ''), COALESCE(l.cwd, '')
		FROM lembranca l
		LEFT JOIN topic_index t ON t.id = l.topic_id
		%s
		ORDER BY l.triggered_at DESC
		LIMIT ?
	`, where)

	rows, err := s.db.Query(sqlStr, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("lembranca query: %w", err)
	}
	defer rows.Close()

	var entries []LembrancaEntry
	for rows.Next() {
		var e LembrancaEntry
		if err := rows.Scan(
			&e.ID, &e.TopicID, &e.TopicTitle, &e.TriggeredAt,
			&e.Kind, &e.Query, &e.Cwd,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// recencyWeight maps "time since most recent lembrança for a topic" to a
// score boost in [0, 1]. Stepped decay: full weight while recent, drops off
// over hours/days/weeks.
//
// Returns 0 when lastLembrancaMs == 0 (topic never lembrada).
func recencyWeight(lastLembrancaMs, nowMs int64) float64 {
	if lastLembrancaMs == 0 {
		return 0
	}
	age := nowMs - lastLembrancaMs
	if age < 0 {
		age = 0
	}
	const (
		oneHour = 60 * 60 * 1000
		oneDay  = 24 * oneHour
		oneWeek = 7 * oneDay
	)
	switch {
	case age < oneHour:
		return 1.0
	case age < oneDay:
		return 0.5
	case age < oneWeek:
		return 0.1
	default:
		return 0
	}
}
