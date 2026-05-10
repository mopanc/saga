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
	// IncludeSuperseded — when false (default), topics that are the target of
	// an active @supersedes edge are excluded from results. Set to true for
	// audit/debug or to re-surface historical versions.
	IncludeSuperseded bool `json:"include_superseded,omitempty"`
}

type TopicSnippet struct {
	ID            string   `json:"id"`
	Scope         string   `json:"scope"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Synonyms      []string `json:"synonyms"`
	FilePath      string   `json:"file_path"`
	SourceLayer   string   `json:"source_layer"`
	Sensitivity   string   `json:"sensitivity"`
	Confidence    string   `json:"confidence"`
	CreatedAt     int64    `json:"created_at"`
	UpdatedAt     int64    `json:"updated_at"`
	Score         float64  `json:"score"`
	ConflictsWith []string `json:"conflicts_with,omitempty"`
}

// refinesScoreBoost — score bump applied to a topic that @refines another.
// The refiner is more specific / current; both remain injectable (unlike
// supersedes which excludes the target). Boost is small enough not to
// override a clear BM25 winner, large enough to break ties.
const refinesScoreBoost = 0.1

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

	// Apply relation-aware semantics:
	//   @supersedes  — exclude target from default recall (S0-3)
	//   @refines     — boost source score (S0-5)
	//   @conflicts_with — annotate both sides (S0-4)
	//
	// One query covers all three. Cheap: indexed by source_id and target_id;
	// scoped to the candidate set, not the whole DB.
	if len(candidates) > 0 {
		filtered, err := s.applyRelations(candidates, args.IncludeSuperseded)
		if err != nil {
			return nil, fmt.Errorf("apply relations: %w", err)
		}
		candidates = filtered
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

// applyRelations filters and decorates the candidate set using topic_relation
// edges. See spec §6.2 for operator semantics.
func (s *Service) applyRelations(candidates []TopicSnippet, includeSuperseded bool) ([]TopicSnippet, error) {
	ids := make([]any, 0, len(candidates))
	idIndex := make(map[string]int, len(candidates))
	for i, c := range candidates {
		ids = append(ids, c.ID)
		idIndex[c.ID] = i
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	// We pass the candidate ids twice so source_id IN (...) OR target_id IN (...).
	args := append(append([]any{}, ids...), ids...)
	q := fmt.Sprintf(`
		SELECT source_id, op, target_id
		FROM topic_relation
		WHERE source_id IN (%s) OR target_id IN (%s)
	`, placeholders, placeholders)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	supersededTargets := map[string]bool{}
	refinerSources := map[string]bool{}
	conflicts := map[string]map[string]bool{} // dedupe via inner set
	for rows.Next() {
		var src, op, tgt string
		if err := rows.Scan(&src, &op, &tgt); err != nil {
			return nil, err
		}
		switch op {
		case "@supersedes":
			supersededTargets[tgt] = true
		case "@refines":
			refinerSources[src] = true
		case "@conflicts_with":
			if _, ok := idIndex[src]; ok {
				if conflicts[src] == nil {
					conflicts[src] = map[string]bool{}
				}
				conflicts[src][tgt] = true
			}
			if _, ok := idIndex[tgt]; ok {
				if conflicts[tgt] == nil {
					conflicts[tgt] = map[string]bool{}
				}
				conflicts[tgt][src] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]TopicSnippet, 0, len(candidates))
	for _, c := range candidates {
		if !includeSuperseded && supersededTargets[c.ID] {
			continue
		}
		if refinerSources[c.ID] {
			c.Score += refinesScoreBoost
		}
		if peers := conflicts[c.ID]; len(peers) > 0 {
			c.ConflictsWith = make([]string, 0, len(peers))
			for p := range peers {
				c.ConflictsWith = append(c.ConflictsWith, p)
			}
			sort.Strings(c.ConflictsWith) // deterministic for tests
		}
		out = append(out, c)
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// conflicts (S0-4)
// ----------------------------------------------------------------------------

// ConflictPair — a pair of topics linked by @conflicts_with. Pairs are
// deduplicated regardless of which side declared the relation first.
type ConflictPair struct {
	A    TopicSummary `json:"a"`
	B    TopicSummary `json:"b"`
	Note string       `json:"note,omitempty"`
}

// ListConflicts returns one entry per unique pair across all active layers.
// Used by `saga conflicts` and by `saga health` (future).
func (s *Service) ListConflicts() ([]ConflictPair, error) {
	layers, err := s.resolver.Resolve(s.cwd)
	if err != nil {
		return nil, err
	}
	if len(layers) == 0 {
		return nil, nil
	}
	scopes := make([]any, 0, len(layers))
	for _, l := range layers {
		scopes = append(scopes, l.Scope)
	}
	placeholders := strings.Repeat("?,", len(scopes))
	placeholders = placeholders[:len(placeholders)-1]

	// Order pairs canonically (lower id first) so reciprocal declarations
	// collapse to one row regardless of who declared the relation.
	q := fmt.Sprintf(`
		SELECT DISTINCT
		       MIN(tr.source_id, tr.target_id) AS lo,
		       MAX(tr.source_id, tr.target_id) AS hi,
		       COALESCE(tr.note, '')
		FROM topic_relation tr
		JOIN topic_index a ON a.id = tr.source_id AND a.scope IN (%s)
		JOIN topic_index b ON b.id = tr.target_id AND b.scope IN (%s)
		WHERE tr.op = '@conflicts_with'
	`, placeholders, placeholders)
	args := append(append([]any{}, scopes...), scopes...)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type pairKey struct{ lo, hi string }
	pairs := map[pairKey]string{}
	for rows.Next() {
		var lo, hi, note string
		if err := rows.Scan(&lo, &hi, &note); err != nil {
			return nil, err
		}
		key := pairKey{lo, hi}
		if existing, ok := pairs[key]; !ok || (existing == "" && note != "") {
			pairs[key] = note
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, nil
	}

	// Fetch summary rows for everything we need in a single batch.
	idSet := make(map[string]bool, len(pairs)*2)
	for k := range pairs {
		idSet[k.lo] = true
		idSet[k.hi] = true
	}
	summaries, err := s.lookupSummaries(idSet)
	if err != nil {
		return nil, err
	}

	out := make([]ConflictPair, 0, len(pairs))
	for k, note := range pairs {
		a, ok1 := summaries[k.lo]
		b, ok2 := summaries[k.hi]
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, ConflictPair{A: a, B: b, Note: note})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].A.Title != out[j].A.Title {
			return out[i].A.Title < out[j].A.Title
		}
		return out[i].B.Title < out[j].B.Title
	})
	return out, nil
}

func (s *Service) lookupSummaries(idSet map[string]bool) (map[string]TopicSummary, error) {
	if len(idSet) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(idSet))
	for id := range idSet {
		args = append(args, id)
	}
	placeholders := strings.Repeat("?,", len(args))
	placeholders = placeholders[:len(placeholders)-1]
	q := fmt.Sprintf(`
		SELECT id, scope, type, title, source_layer, updated_at
		FROM topic_index
		WHERE id IN (%s)
	`, placeholders)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TopicSummary{}
	for rows.Next() {
		var t TopicSummary
		if err := rows.Scan(&t.ID, &t.Scope, &t.Type, &t.Title, &t.SourceLayer, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out[t.ID] = t
	}
	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// show (chains for S0-3 + S0-5)
// ----------------------------------------------------------------------------

// ShowRelation describes one edge participating in a topic, with the other
// end resolved to a summary when known.
type ShowRelation struct {
	Direction string        `json:"direction"` // "out" | "in"
	Op        string        `json:"op"`
	OtherID   string        `json:"other_id"`
	Other     *TopicSummary `json:"other,omitempty"`
	Note      string        `json:"note,omitempty"`
}

type ShowResult struct {
	Topic     *Topic         `json:"topic"`
	Relations []ShowRelation `json:"relations,omitempty"`
}

// Show resolves a topic by id-or-slug-or-title and returns it plus all its
// relations (outgoing and incoming), with the other end resolved when known.
func (s *Service) Show(idOrSlug string) (*ShowResult, error) {
	if idOrSlug == "" {
		return nil, fmt.Errorf("id or slug is required")
	}
	slugMatch := "%/" + Slugify(idOrSlug) + ".md"
	var path, id string
	err := s.db.QueryRow(`
		SELECT id, file_path FROM topic_index
		WHERE id = ? OR title = ? OR file_path LIKE ?
		LIMIT 1
	`, idOrSlug, idOrSlug, slugMatch).Scan(&id, &path)
	if err != nil {
		return nil, fmt.Errorf("topic %q not found", idOrSlug)
	}
	content, err := os.ReadFile(path) // #nosec G304 -- path comes from saga's own indexed topic_index
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	topic, err := ParseTopic(content)
	if err != nil {
		return nil, err
	}

	// Outgoing + incoming edges in one query for symmetry.
	rows, err := s.db.Query(`
		SELECT 'out' AS dir, op, target_id, COALESCE(note, '') FROM topic_relation WHERE source_id = ?
		UNION ALL
		SELECT 'in'  AS dir, op, source_id, COALESCE(note, '') FROM topic_relation WHERE target_id = ?
	`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rels []ShowRelation
	otherIDs := map[string]bool{}
	for rows.Next() {
		var r ShowRelation
		if err := rows.Scan(&r.Direction, &r.Op, &r.OtherID, &r.Note); err != nil {
			return nil, err
		}
		rels = append(rels, r)
		otherIDs[r.OtherID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Resolve other-end summaries; dangling targets stay as IDs only.
	if len(otherIDs) > 0 {
		ids := make([]any, 0, len(otherIDs))
		for k := range otherIDs {
			ids = append(ids, k)
		}
		ph := strings.Repeat("?,", len(ids))
		ph = ph[:len(ph)-1]
		q := fmt.Sprintf(`SELECT id, scope, type, title, source_layer, updated_at FROM topic_index WHERE id IN (%s)`, ph)
		rs, err := s.db.Query(q, ids...)
		if err != nil {
			return nil, err
		}
		summaries := map[string]TopicSummary{}
		for rs.Next() {
			var t TopicSummary
			if err := rs.Scan(&t.ID, &t.Scope, &t.Type, &t.Title, &t.SourceLayer, &t.UpdatedAt); err != nil {
				_ = rs.Close()
				return nil, err
			}
			summaries[t.ID] = t
		}
		_ = rs.Close()
		for i := range rels {
			if s, ok := summaries[rels[i].OtherID]; ok {
				ss := s
				rels[i].Other = &ss
			}
		}
	}

	return &ShowResult{Topic: topic, Relations: rels}, nil
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
	content, err := os.ReadFile(path) // #nosec G304 -- path is from saga's own topic_index DB lookup
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

// MaxTopicBodyChars caps the size of a topic body (after append assembly).
// Roughly 2000 tokens / 3000 words — generous for a self-contained topic,
// strict enough to prevent the AI from writing monolithic notes that would
// dominate context budgets when read back later.
const MaxTopicBodyChars = 8000

type TopicWriteArgs struct {
	Name       string           `json:"name"`
	Scope      string           `json:"scope,omitempty"`
	Title      string           `json:"title,omitempty"`
	Synonyms   []string         `json:"synonyms,omitempty"`
	Body       string           `json:"body"`
	Mode       string           `json:"mode,omitempty"` // create|append|replace
	References []TopicReference `json:"references,omitempty"`
	Type       string           `json:"type,omitempty"` // default: topic
	// AllowSecret bypasses the secret-pattern check. Use ONLY when knowingly
	// persisting credential-shaped strings (e.g. a topic ABOUT a token's
	// format). Logged for audit when set.
	AllowSecret bool `json:"allow_secret,omitempty"`
	// ForceDuplicate suppresses the similarity warning. Set when a topic is
	// genuinely new despite high title overlap with an existing one.
	ForceDuplicate bool `json:"force_duplicate,omitempty"`
}

type TopicWriteResult struct {
	ID      string             `json:"id"`
	Path    string             `json:"path"`
	Scope   string             `json:"scope"`
	Action  string             `json:"action"`
	Warning *TopicWriteWarning `json:"warning,omitempty"`
}

// TopicWriteWarning is returned alongside a successful write when the engine
// detected something the caller might want to act on (similar existing
// topic, etc). Non-fatal; the topic is persisted regardless.
type TopicWriteWarning struct {
	Kind       string         `json:"kind"`
	Candidates []TopicSummary `json:"candidates,omitempty"`
	Hint       string         `json:"hint,omitempty"`
}

// titleSimilarityWarn — Jaccard index threshold above which we surface a
// similarity warning. Tuned empirically: bag-of-words overlap of 0.6+
// reliably catches near-duplicates without false-positiving on topic-grain
// notes that share a domain term.
const titleSimilarityWarn = 0.6

func (s *Service) TopicWrite(args TopicWriteArgs) (*TopicWriteResult, error) {
	if args.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if args.Body == "" {
		return nil, fmt.Errorf("body is required")
	}
	if len(args.Body) > MaxTopicBodyChars {
		return nil, fmt.Errorf(
			"body too large: %d chars (cap is %d, ~2000 tokens). Split into multiple narrower topics, or call topic_write with mode=append on an existing topic to add a dated section",
			len(args.Body), MaxTopicBodyChars,
		)
	}
	if !args.AllowSecret {
		if hits := DetectSecrets(args.Body); len(hits) > 0 {
			return nil, fmt.Errorf(
				"secret pattern detected (%s at line %d) — topic not written; "+
					"remove the credential from the body, or set allow_secret=true if "+
					"this topic is intentionally about credential formats",
				hits[0].Kind, hits[0].Line,
			)
		}
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
	if data, err := os.ReadFile(fpath); err == nil { // #nosec G304 -- fpath is constructed inside NotesDir via Slugify (regex-safe: ^[a-z0-9-]+$)
		if t, err := ParseTopic(data); err == nil {
			existing = t
		}
	}

	// Pre-write similarity check: warn (non-blocking) if another topic in the
	// same scope has a near-duplicate title. The caller decides what to do —
	// proceed (warning ignored), restart with @supersedes / @refines, or set
	// force_duplicate=true to suppress on subsequent calls.
	var warning *TopicWriteWarning
	if !args.ForceDuplicate {
		excludeID := ""
		if existing != nil {
			excludeID = existing.ID
		}
		candidates, err := s.findSimilarTopics(title, scope, excludeID)
		if err != nil {
			return nil, fmt.Errorf("similarity check: %w", err)
		}
		if len(candidates) > 0 {
			warning = &TopicWriteWarning{
				Kind:       "similar_topic_found",
				Candidates: candidates,
				Hint:       "consider @supersedes <id> | @refines <id> | proceed if genuinely new (set force_duplicate=true to suppress)",
			}
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

	if len(topic.Body) > MaxTopicBodyChars {
		return nil, fmt.Errorf(
			"resulting body too large: %d chars (cap is %d, ~2000 tokens). The existing topic %q has grown beyond the per-topic limit; create a new narrower topic instead of appending further",
			len(topic.Body), MaxTopicBodyChars, args.Name,
		)
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
		ID: topic.ID, Path: fpath, Scope: scope, Action: action, Warning: warning,
	}, nil
}

// findSimilarTopics returns topics in the same scope whose title has Jaccard
// overlap above titleSimilarityWarn with the candidate title. Self (matched
// by id) is excluded so updates don't warn against themselves.
//
// Full-scan is acceptable at v1 (single layer, ~hundreds of topics). Switch
// to an FTS5 prefilter if a layer crosses ~10k topics.
func (s *Service) findSimilarTopics(title, scope, excludeID string) ([]TopicSummary, error) {
	rows, err := s.db.Query(`
		SELECT id, scope, type, title, source_layer, updated_at
		FROM topic_index
		WHERE scope = ?
	`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []TopicSummary
	for rows.Next() {
		var t TopicSummary
		if err := rows.Scan(&t.ID, &t.Scope, &t.Type, &t.Title, &t.SourceLayer, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if t.ID == excludeID {
			continue
		}
		if titleJaccard(title, t.Title) >= titleSimilarityWarn {
			hits = append(hits, t)
		}
	}
	return hits, rows.Err()
}

// titleJaccard returns |A ∩ B| / |A ∪ B| over case-folded word sets. Words
// are split on non-letter/digit boundaries; common punctuation drops out.
// Empty inputs yield 0.
func titleJaccard(a, b string) float64 {
	aw := titleTokens(a)
	bw := titleTokens(b)
	if len(aw) == 0 || len(bw) == 0 {
		return 0
	}
	intersect := 0
	for w := range aw {
		if bw[w] {
			intersect++
		}
	}
	union := len(aw) + len(bw) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

func titleTokens(s string) map[string]bool {
	out := map[string]bool{}
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			out[strings.ToLower(cur.String())] = true
			cur.Reset()
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
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
