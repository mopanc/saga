package saga

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Service is the application-level facade: layer-aware retrieval, on-disk
// topic file management, atomic write + index update.
type Service struct {
	db       *DB
	resolver *Resolver
	cwd      string
}

func NewService(db *DB, cfg *Config, cwd string) *Service {
	return &Service{db: db, resolver: NewResolver(cfg), cwd: cwd}
}

// ----------------------------------------------------------------------------
// recall
// ----------------------------------------------------------------------------

type RecallArgs struct {
	Query string `json:"query"`
	K     int    `json:"k,omitempty"`
	Scope string `json:"scope,omitempty"`
	Type  string `json:"type,omitempty"`
}

type TopicSnippet struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Synonyms    []string `json:"synonyms"`
	FilePath    string   `json:"file_path"`
	SourceLayer string   `json:"source_layer"`
	Sensitivity string   `json:"sensitivity"`
	Confidence  string   `json:"confidence"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	Score       float64  `json:"score"`
}

func (s *Service) Recall(args RecallArgs) ([]TopicSnippet, error) {
	if args.Query == "" {
		return nil, nil
	}
	layers, err := s.resolver.Resolve(s.cwd)
	if err != nil {
		return nil, err
	}
	if len(layers) == 0 {
		return nil, nil
	}
	k := args.K
	if k <= 0 {
		k = 3
	}
	if k > 50 {
		k = 50
	}
	ftsQuery := sanitizeFTSQuery(args.Query)
	if ftsQuery == "" {
		return nil, nil
	}

	var scopes []string
	if args.Scope != "" {
		scopes = []string{args.Scope}
	} else {
		for _, l := range layers {
			scopes = append(scopes, l.Scope)
		}
	}

	// Over-fetch by 3x so re-ranking with recency has enough candidates.
	// SQL orders by BM25; Go re-orders by combined score; we then trim to k.
	overfetch := k * 3
	if overfetch < 10 {
		overfetch = 10
	}

	placeholders := strings.Repeat("?,", len(scopes))
	placeholders = placeholders[:len(placeholders)-1]
	qArgs := []any{ftsQuery}
	for _, sc := range scopes {
		qArgs = append(qArgs, sc)
	}
	typeClause := ""
	if args.Type != "" {
		typeClause = " AND t.type = ?"
		qArgs = append(qArgs, args.Type)
	}
	qArgs = append(qArgs, overfetch)

	sqlStr := fmt.Sprintf(`
		SELECT t.id, t.scope, t.type, t.title, t.synonyms, t.file_path,
		       t.source_layer, t.sensitivity, t.confidence,
		       t.created_at, t.updated_at,
		       bm25(topic_fts) AS bm25_score,
		       COALESCE((SELECT MAX(triggered_at) FROM lembranca l WHERE l.topic_id = t.id), 0)
		         AS last_lembranca
		FROM topic_fts
		JOIN topic_index t ON t.id = topic_fts.id
		WHERE topic_fts MATCH ? AND t.scope IN (%s)%s
		ORDER BY bm25(topic_fts) ASC
		LIMIT ?
	`, placeholders, typeClause)

	rows, err := s.db.Query(sqlStr, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("recall query: %w", err)
	}
	defer rows.Close()

	now := time.Now().UnixMilli()
	var candidates []TopicSnippet
	for rows.Next() {
		var snip TopicSnippet
		var synJSON string
		var bm25 float64
		var lastLembranca int64
		if err := rows.Scan(
			&snip.ID, &snip.Scope, &snip.Type, &snip.Title, &synJSON,
			&snip.FilePath, &snip.SourceLayer, &snip.Sensitivity, &snip.Confidence,
			&snip.CreatedAt, &snip.UpdatedAt, &bm25, &lastLembranca,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(synJSON), &snip.Synonyms)
		// bm25: lower=better; flip + add recency boost so higher=better.
		snip.Score = -bm25 + recencyWeight(lastLembranca, now)
		candidates = append(candidates, snip)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Re-rank by combined score (descending) and trim to k.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	return candidates, nil
}

// ----------------------------------------------------------------------------
// topic.read
// ----------------------------------------------------------------------------

type TopicReadArgs struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

func (s *Service) TopicRead(args TopicReadArgs) (*Topic, error) {
	if args.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	slugMatch := "%/" + Slugify(args.Name) + ".md"
	var path string
	var err error
	if args.Scope != "" {
		err = s.db.QueryRow(
			"SELECT file_path FROM topic_index WHERE scope = ? AND (title = ? OR file_path LIKE ?) LIMIT 1",
			args.Scope, args.Name, slugMatch,
		).Scan(&path)
	} else {
		err = s.db.QueryRow(
			"SELECT file_path FROM topic_index WHERE title = ? OR file_path LIKE ? LIMIT 1",
			args.Name, slugMatch,
		).Scan(&path)
	}
	if err != nil {
		return nil, fmt.Errorf("topic %q not found", args.Name)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return ParseTopic(content)
}

// ----------------------------------------------------------------------------
// topic.list
// ----------------------------------------------------------------------------

type TopicListArgs struct {
	Scope string `json:"scope,omitempty"`
	Type  string `json:"type,omitempty"`
}

type TopicSummary struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	SourceLayer string `json:"source_layer"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (s *Service) TopicList(args TopicListArgs) ([]TopicSummary, error) {
	layers, err := s.resolver.Resolve(s.cwd)
	if err != nil {
		return nil, err
	}
	var scopes []string
	if args.Scope != "" {
		scopes = []string{args.Scope}
	} else {
		for _, l := range layers {
			scopes = append(scopes, l.Scope)
		}
	}
	if len(scopes) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(scopes))
	placeholders = placeholders[:len(placeholders)-1]
	qArgs := []any{}
	for _, sc := range scopes {
		qArgs = append(qArgs, sc)
	}
	typeClause := ""
	if args.Type != "" {
		typeClause = " AND type = ?"
		qArgs = append(qArgs, args.Type)
	}
	sqlStr := fmt.Sprintf(`
		SELECT id, scope, type, title, source_layer, updated_at
		FROM topic_index
		WHERE scope IN (%s)%s
		ORDER BY updated_at DESC
	`, placeholders, typeClause)

	rows, err := s.db.Query(sqlStr, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TopicSummary
	for rows.Next() {
		var t TopicSummary
		if err := rows.Scan(&t.ID, &t.Scope, &t.Type, &t.Title, &t.SourceLayer, &t.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

// ----------------------------------------------------------------------------
// topic.write
// ----------------------------------------------------------------------------

type TopicWriteArgs struct {
	Name       string           `json:"name"`
	Scope      string           `json:"scope,omitempty"`
	Title      string           `json:"title,omitempty"`
	Synonyms   []string         `json:"synonyms,omitempty"`
	Body       string           `json:"body"`
	Mode       string           `json:"mode,omitempty"` // create|append|replace
	References []TopicReference `json:"references,omitempty"`
	Type       string           `json:"type,omitempty"` // default: topic
}

type TopicWriteResult struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	Action string `json:"action"`
}

func (s *Service) TopicWrite(args TopicWriteArgs) (*TopicWriteResult, error) {
	if args.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if args.Body == "" {
		return nil, fmt.Errorf("body is required")
	}
	layers, err := s.resolver.Resolve(s.cwd)
	if err != nil {
		return nil, err
	}
	scope := args.Scope
	if scope == "" {
		scope = "personal"
	}
	var target *Layer
	for i := range layers {
		if layers[i].Scope == scope {
			target = &layers[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("scope %q not active in current context (active: %v)", scope, scopeNames(layers))
	}
	typ := args.Type
	if typ == "" {
		typ = "topic"
	}
	title := args.Title
	if title == "" {
		title = args.Name
	}

	if err := os.MkdirAll(target.NotesDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir notes dir: %w", err)
	}
	fpath := filepath.Join(target.NotesDir, Slugify(args.Name)+".md")

	var existing *Topic
	if data, err := os.ReadFile(fpath); err == nil {
		if t, err := ParseTopic(data); err == nil {
			existing = t
		}
	}

	mode := args.Mode
	if mode == "" {
		if existing != nil {
			mode = "append"
		} else {
			mode = "create"
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	var topic Topic
	var action string

	switch mode {
	case "create":
		if existing != nil {
			return nil, fmt.Errorf("topic %q already exists in scope %q (use mode=append or replace)", args.Name, scope)
		}
		topic = newTopic(scope, typ, title, args.Synonyms, args.References, target.Meta.SensitivityDefault, now, args.Body)
		action = "created"
	case "replace":
		if existing == nil {
			return nil, fmt.Errorf("topic %q does not exist (use mode=create)", args.Name)
		}
		topic = *existing
		topic.Body = args.Body
		topic.UpdatedAt = now
		if len(args.Synonyms) > 0 {
			topic.Synonyms = args.Synonyms
		}
		if len(args.References) > 0 {
			topic.References = args.References
		}
		action = "replaced"
	case "append":
		if existing == nil {
			topic = newTopic(scope, typ, title, args.Synonyms, args.References, target.Meta.SensitivityDefault, now, args.Body)
			action = "created"
		} else {
			topic = *existing
			sep := "\n\n## Update " + now.Format("2006-01-02") + "\n\n"
			topic.Body = strings.TrimRight(topic.Body, "\n") + sep + args.Body
			topic.UpdatedAt = now
			if len(args.Synonyms) > 0 {
				topic.Synonyms = mergeStrings(topic.Synonyms, args.Synonyms)
			}
			if len(args.References) > 0 {
				topic.References = mergeRefs(topic.References, args.References)
			}
			action = "appended"
		}
	default:
		return nil, fmt.Errorf("invalid mode %q (want create|append|replace)", mode)
	}

	rendered, err := topic.Render()
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	if err := writeFileAtomic(fpath, rendered, 0o600); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	if err := s.db.indexFile(fpath, *target); err != nil {
		return nil, fmt.Errorf("reindex: %w", err)
	}
	return &TopicWriteResult{
		ID: topic.ID, Path: fpath, Scope: scope, Action: action,
	}, nil
}

func newTopic(scope, typ, title string, syn []string, refs []TopicReference, sensDefault string, now time.Time, body string) Topic {
	sens := sensDefault
	if sens == "" {
		sens = "internal"
	}
	return Topic{
		ID:          ulid.Make().String(),
		Scope:       scope,
		Type:        typ,
		Title:       title,
		Synonyms:    syn,
		Sensitivity: sens,
		Confidence:  "proposed",
		CreatedAt:   now,
		UpdatedAt:   now,
		References:  refs,
		Body:        body,
	}
}

func mergeStrings(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(a, b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func mergeRefs(a, b []TopicReference) []TopicReference {
	seen := map[string]bool{}
	var out []TopicReference
	for _, r := range append(a, b...) {
		key := r.Path + ":" + r.Lines
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	return out
}

func scopeNames(layers []Layer) []string {
	out := make([]string, len(layers))
	for i, l := range layers {
		out[i] = l.Scope
	}
	return out
}

// CountTopics returns the total number of indexed topic notes across all
// scopes. Used by the always-on hook to emit a stable <saga-meta> bootstrap
// block — sessions need to know the saga is wired in even when it is empty.
func (s *Service) CountTopics() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM topic_index`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
