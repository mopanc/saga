package saga

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupServiceTest(t *testing.T) (*Service, *DB) {
	t.Helper()
	cfg, _ := withTempHome(t)
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	repo := t.TempDir()
	sagaDir := filepath.Join(repo, ".saga")
	if err := os.MkdirAll(filepath.Join(sagaDir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := []byte("scope: project:demo\nwrite_policy: direct\nnotes_dir: topics/\n")
	if err := os.WriteFile(filepath.Join(sagaDir, "meta.yml"), meta, 0o644); err != nil {
		t.Fatal(err)
	}

	return NewService(db, cfg, repo), db
}

func TestService_TopicWrite_create(t *testing.T) {
	svc, db := setupServiceTest(t)

	res, err := svc.TopicWrite(TopicWriteArgs{
		Name:  "MJPEG performance",
		Scope: "project:demo",
		Body:  "Initial investigation notes.",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if res.Action != "created" {
		t.Errorf("action = %q, want created", res.Action)
	}
	if !strings.HasSuffix(res.Path, "mjpeg-performance.md") {
		t.Errorf("path = %q", res.Path)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Errorf("file: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_index WHERE scope = 'project:demo'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("indexed count = %d, want 1", count)
	}
}

func TestService_TopicWrite_appendDefault(t *testing.T) {
	svc, _ := setupServiceTest(t)

	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "demo", Scope: "project:demo", Body: "first",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TopicWrite(TopicWriteArgs{
		Name: "demo", Scope: "project:demo", Body: "second update",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "appended" {
		t.Errorf("action = %q, want appended", res.Action)
	}
	content, _ := os.ReadFile(res.Path)
	s := string(content)
	if !strings.Contains(s, "first") {
		t.Errorf("body missing 'first':\n%s", s)
	}
	if !strings.Contains(s, "second update") {
		t.Errorf("body missing 'second update':\n%s", s)
	}
	if !strings.Contains(s, "## Update ") {
		t.Errorf("body missing append separator:\n%s", s)
	}
}

func TestService_TopicWrite_createDuplicateRejected(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "demo", Scope: "project:demo", Body: "x", Mode: "create",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "demo", Scope: "project:demo", Body: "y", Mode: "create",
	}); err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestService_TopicWrite_bodyAtCapAccepted(t *testing.T) {
	svc, _ := setupServiceTest(t)
	body := strings.Repeat("x", MaxTopicBodyChars)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "at-cap", Scope: "project:demo", Body: body,
	}); err != nil {
		t.Fatalf("body of exactly MaxTopicBodyChars must be accepted: %v", err)
	}
}

func TestService_TopicWrite_bodyOverCapRejected(t *testing.T) {
	svc, _ := setupServiceTest(t)
	body := strings.Repeat("x", MaxTopicBodyChars+1)
	_, err := svc.TopicWrite(TopicWriteArgs{
		Name: "over-cap", Scope: "project:demo", Body: body,
	})
	if err == nil {
		t.Fatal("expected error for body > MaxTopicBodyChars")
	}
	if !strings.Contains(err.Error(), "body too large") {
		t.Errorf("error should mention 'body too large', got: %v", err)
	}
	if !strings.Contains(err.Error(), "Split") && !strings.Contains(err.Error(), "split") {
		t.Errorf("error should suggest splitting, got: %v", err)
	}
}

func TestService_TopicWrite_appendCannotExceedCap(t *testing.T) {
	svc, _ := setupServiceTest(t)
	half := strings.Repeat("x", MaxTopicBodyChars-100)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "growing", Scope: "project:demo", Body: half, Mode: "create",
	}); err != nil {
		t.Fatal(err)
	}
	// First append fits.
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "growing", Scope: "project:demo", Body: "small", Mode: "append",
	}); err != nil {
		t.Fatalf("small append should fit: %v", err)
	}
	// Second append blows past the cap.
	bigAppend := strings.Repeat("y", 200)
	_, err := svc.TopicWrite(TopicWriteArgs{
		Name: "growing", Scope: "project:demo", Body: bigAppend, Mode: "append",
	})
	if err == nil {
		t.Fatal("append that pushes final body over cap should be rejected")
	}
	if !strings.Contains(err.Error(), "resulting body too large") {
		t.Errorf("error should mention 'resulting body too large', got: %v", err)
	}
}

func TestService_TopicWrite_unknownScopeRejected(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "dept:not-loaded", Body: "y",
	}); err == nil {
		t.Error("expected error on unknown scope")
	}
}

func TestService_TopicWrite_secretBlockedByDefault(t *testing.T) {
	svc, _ := setupServiceTest(t)
	_, err := svc.TopicWrite(TopicWriteArgs{
		Name:  "credentials note",
		Scope: "personal",
		Body:  "the prod key is AKIAIOSFODNN7EXAMPLE — rotate weekly",
	})
	if err == nil {
		t.Fatal("expected secret-block error")
	}
	if !strings.Contains(err.Error(), "secret pattern detected") {
		t.Errorf("error = %q, want secret-pattern message", err.Error())
	}
}

func TestService_TopicWrite_secretAllowedWithFlag(t *testing.T) {
	svc, _ := setupServiceTest(t)
	_, err := svc.TopicWrite(TopicWriteArgs{
		Name:        "credentials format note",
		Scope:       "personal",
		Body:        "AKIA prefix marks AWS access keys (e.g. AKIAIOSFODNN7EXAMPLE).",
		AllowSecret: true,
	})
	if err != nil {
		t.Fatalf("AllowSecret=true should bypass detection: %v", err)
	}
}

func TestService_TopicWrite_similarityWarning(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "stream cache for SSE", Scope: "personal", Body: "first version",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TopicWrite(TopicWriteArgs{
		Name: "stream cache for sse handlers", Scope: "personal", Body: "second variant",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning == nil {
		t.Fatal("expected a similarity warning when titles share most tokens")
	}
	if res.Warning.Kind != "similar_topic_found" {
		t.Errorf("warning kind = %q", res.Warning.Kind)
	}
	if len(res.Warning.Candidates) == 0 {
		t.Error("warning has no candidates")
	}
	if res.Warning.Hint == "" {
		t.Error("warning hint is empty")
	}
}

func TestService_TopicWrite_similarityNoWarnOnDistinctTitles(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "stream cache base", Scope: "personal", Body: "x",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TopicWrite(TopicWriteArgs{
		Name: "auth flow rewrite", Scope: "personal", Body: "y",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != nil {
		t.Errorf("unexpected warning between distinct titles: %+v", res.Warning)
	}
}

func TestService_TopicWrite_similaritySuppressedByForceDuplicate(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "stream cache for SSE", Scope: "personal", Body: "first",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TopicWrite(TopicWriteArgs{
		Name:           "stream cache for sse handlers",
		Scope:          "personal",
		Body:           "second",
		ForceDuplicate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != nil {
		t.Errorf("ForceDuplicate=true should suppress; got %+v", res.Warning)
	}
}

func TestService_TopicWrite_similarityIgnoresSelfOnUpdate(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "stream cache base", Scope: "personal", Body: "first",
	}); err != nil {
		t.Fatal(err)
	}
	// Append-mode (default) on the same name reads the existing topic — the
	// similarity check must not warn about the topic being updated.
	res, err := svc.TopicWrite(TopicWriteArgs{
		Name: "stream cache base", Scope: "personal", Body: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Warning != nil {
		t.Errorf("self update warned about itself: %+v", res.Warning)
	}
}

func TestTitleJaccard_basicShape(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"", "", 0},
		{"identical", "identical", 1.0},
		{"foo bar", "foo bar baz", float64(2) / float64(3)},
		{"foo", "bar", 0},
	}
	for _, c := range cases {
		got := titleJaccard(c.a, c.b)
		if got != c.want {
			t.Errorf("titleJaccard(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestService_TopicWrite_replace(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "personal", Body: "v1",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "personal", Body: "v2", Mode: "replace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "replaced" {
		t.Errorf("action = %q", res.Action)
	}
	content, _ := os.ReadFile(res.Path)
	if strings.Contains(string(content), "v1") {
		t.Errorf("replace did not remove old body:\n%s", content)
	}
}

func TestService_Recall_filtersByScope(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "mjpeg perf", Scope: "project:demo", Body: "Stream lento por causa do socket.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "private note", Scope: "personal", Body: "Stream pessoal lento.",
	}); err != nil {
		t.Fatal(err)
	}

	all, err := svc.Recall(RecallArgs{Query: "stream"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("got %d, want 2", len(all))
	}

	only, err := svc.Recall(RecallArgs{Query: "stream", Scope: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 {
		t.Errorf("got %d, want 1", len(only))
	}
	if only[0].Scope != "personal" {
		t.Errorf("scope = %q", only[0].Scope)
	}
}

func TestService_Recall_kLimit(t *testing.T) {
	svc, _ := setupServiceTest(t)
	for i := 0; i < 5; i++ {
		name := "note-" + string(rune('a'+i))
		if _, err := svc.TopicWrite(TopicWriteArgs{
			Name: name, Scope: "personal", Body: "saga note saga test", Mode: "create",
		}); err != nil {
			t.Fatal(err)
		}
	}
	results, err := svc.Recall(RecallArgs{Query: "saga", K: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d, want 2", len(results))
	}
}

func TestService_Recall_emptyQueryNoResults(t *testing.T) {
	svc, _ := setupServiceTest(t)
	results, err := svc.Recall(RecallArgs{Query: "***"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d, want 0", len(results))
	}
}

func TestService_TopicRead_byTitleAndSlug(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "xyz", Scope: "personal", Title: "XYZ Topic", Body: "the body",
	}); err != nil {
		t.Fatal(err)
	}

	// By title
	t1, err := svc.TopicRead(TopicReadArgs{Name: "XYZ Topic"})
	if err != nil {
		t.Fatalf("read by title: %v", err)
	}
	if !strings.Contains(t1.Body, "the body") {
		t.Errorf("body = %q", t1.Body)
	}

	// By slug
	t2, err := svc.TopicRead(TopicReadArgs{Name: "xyz"})
	if err != nil {
		t.Fatalf("read by slug: %v", err)
	}
	if t2.ID != t1.ID {
		t.Errorf("ID mismatch: %s vs %s", t1.ID, t2.ID)
	}
}

func TestService_TopicRead_notFound(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicRead(TopicReadArgs{Name: "ghost"}); err == nil {
		t.Error("expected error for missing topic")
	}
}

// setupRelationTest mirrors setupServiceTest but also returns a Layer
// fixture so tests can seed topics with raw frontmatter (relations) directly.
func setupRelationTest(t *testing.T) (*Service, *DB, Layer) {
	t.Helper()
	cfg, _ := withTempHome(t)
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	repo := t.TempDir()
	sagaDir := filepath.Join(repo, ".saga")
	notesDir := filepath.Join(sagaDir, "topics")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := []byte("scope: project:demo\nwrite_policy: direct\nnotes_dir: topics/\n")
	if err := os.WriteFile(filepath.Join(sagaDir, "meta.yml"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	layer := Layer{
		Scope:    "project:demo",
		RootPath: sagaDir,
		NotesDir: notesDir,
		Meta:     Meta{Scope: "project:demo", NotesDir: "topics/"},
	}
	return NewService(db, cfg, repo), db, layer
}

// seedTopic writes a topic .md to disk and reindexes; returns nothing because
// the test only cares the row is in the index.
func seedTopic(t *testing.T, db *DB, layer Layer, slug, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(layer.NotesDir, slug+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatal(err)
	}
}

const topicWithoutRelations = `---
id: 01HAAAAAAAAAAAAAAAAAAAAAAA
scope: project:demo
type: topic
title: stream cache base
sensitivity: internal
confidence: proposed
created_at: 2026-04-01T00:00:00Z
updated_at: 2026-04-01T00:00:00Z
---

stream cache base notes
`

const topicSupersedingBase = `---
id: 01HBBBBBBBBBBBBBBBBBBBBBBB
scope: project:demo
type: topic
title: stream cache redux
sensitivity: internal
confidence: proposed
created_at: 2026-04-15T00:00:00Z
updated_at: 2026-04-15T00:00:00Z
relations:
  - { op: "@supersedes", target: "01HAAAAAAAAAAAAAAAAAAAAAAA" }
---

stream cache redux notes
`

const topicRefiningBase = `---
id: 01HCCCCCCCCCCCCCCCCCCCCCCC
scope: project:demo
type: topic
title: stream cache for SSE
sensitivity: internal
confidence: proposed
created_at: 2026-04-20T00:00:00Z
updated_at: 2026-04-20T00:00:00Z
relations:
  - { op: "@refines", target: "01HAAAAAAAAAAAAAAAAAAAAAAA" }
---

stream cache notes refined for SSE
`

const topicConflictA = `---
id: 01HDDDDDDDDDDDDDDDDDDDDDDD
scope: project:demo
type: preference
title: stream cache prefer redis
sensitivity: internal
confidence: proposed
created_at: 2026-04-22T00:00:00Z
updated_at: 2026-04-22T00:00:00Z
relations:
  - { op: "@conflicts_with", target: "01HEEEEEEEEEEEEEEEEEEEEEEE", note: "swapped to redis April" }
---

prefer redis for stream cache
`

const topicConflictB = `---
id: 01HEEEEEEEEEEEEEEEEEEEEEEE
scope: project:demo
type: preference
title: stream cache prefer memcached
sensitivity: internal
confidence: proposed
created_at: 2026-03-01T00:00:00Z
updated_at: 2026-03-01T00:00:00Z
---

prefer memcached for stream cache
`

func TestService_Recall_excludesSupersededTarget(t *testing.T) {
	svc, db, layer := setupRelationTest(t)
	seedTopic(t, db, layer, "stream-cache-base", topicWithoutRelations)
	seedTopic(t, db, layer, "stream-cache-redux", topicSupersedingBase)

	// Without flag: only the superseder should surface.
	results, err := svc.Recall(RecallArgs{Query: "stream cache"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ID == "01HAAAAAAAAAAAAAAAAAAAAAAA" {
			t.Errorf("base topic should be excluded by @supersedes; got %+v", r)
		}
	}
	if len(results) == 0 {
		t.Fatal("expected at least the superseder in results")
	}

	// With flag: both surface.
	results, err = svc.Recall(RecallArgs{Query: "stream cache", IncludeSuperseded: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range results {
		got[r.ID] = true
	}
	if !got["01HAAAAAAAAAAAAAAAAAAAAAAA"] || !got["01HBBBBBBBBBBBBBBBBBBBBBBB"] {
		t.Errorf("expected both topics with IncludeSuperseded=true; got %v", got)
	}
}

func TestService_Recall_supersedesChain(t *testing.T) {
	svc, db, layer := setupRelationTest(t)
	// A is base, B supersedes A, C supersedes B → only C should be in default recall.
	seedTopic(t, db, layer, "a", topicWithoutRelations)
	seedTopic(t, db, layer, "b", topicSupersedingBase)
	seedTopic(t, db, layer, "c", `---
id: 01HCHAINTOPCHAINTOPCHAINTC
scope: project:demo
type: topic
title: stream cache final
sensitivity: internal
confidence: proposed
created_at: 2026-05-01T00:00:00Z
updated_at: 2026-05-01T00:00:00Z
relations:
  - { op: "@supersedes", target: "01HBBBBBBBBBBBBBBBBBBBBBBB" }
---

final
`)

	results, err := svc.Recall(RecallArgs{Query: "stream cache"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ID == "01HAAAAAAAAAAAAAAAAAAAAAAA" || r.ID == "01HBBBBBBBBBBBBBBBBBBBBBBB" {
			t.Errorf("chained superseded topic surfaced unexpectedly: %s", r.ID)
		}
	}
	found := false
	for _, r := range results {
		if r.ID == "01HCHAINTOPCHAINTOPCHAINTC" {
			found = true
		}
	}
	if !found {
		t.Errorf("chain leaf should be present in results")
	}
}

func TestService_Recall_refinesBoost(t *testing.T) {
	svc, db, layer := setupRelationTest(t)
	seedTopic(t, db, layer, "stream-cache-base", topicWithoutRelations)
	seedTopic(t, db, layer, "stream-cache-for-sse", topicRefiningBase)

	results, err := svc.Recall(RecallArgs{Query: "stream cache"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("expected both topics in results, got %d", len(results))
	}
	// Refiner should be ranked higher (or at least not lower) than refined.
	var baseIdx, refinerIdx int = -1, -1
	for i, r := range results {
		if r.ID == "01HAAAAAAAAAAAAAAAAAAAAAAA" {
			baseIdx = i
		}
		if r.ID == "01HCCCCCCCCCCCCCCCCCCCCCCC" {
			refinerIdx = i
		}
	}
	if baseIdx < 0 || refinerIdx < 0 {
		t.Fatalf("missing expected topics; baseIdx=%d refinerIdx=%d", baseIdx, refinerIdx)
	}
	if refinerIdx > baseIdx {
		t.Errorf("refiner should rank ≥ base after +%.1f boost; got refiner@%d, base@%d (scores: %v vs %v)",
			refinesScoreBoost, refinerIdx, baseIdx, results[refinerIdx].Score, results[baseIdx].Score)
	}
}

func TestService_Recall_surfacesConflicts(t *testing.T) {
	svc, db, layer := setupRelationTest(t)
	seedTopic(t, db, layer, "stream-cache-prefer-redis", topicConflictA)
	seedTopic(t, db, layer, "stream-cache-prefer-memcached", topicConflictB)

	results, err := svc.Recall(RecallArgs{Query: "stream cache prefer"})
	if err != nil {
		t.Fatal(err)
	}
	annotated := map[string][]string{}
	for _, r := range results {
		if len(r.ConflictsWith) > 0 {
			annotated[r.ID] = r.ConflictsWith
		}
	}
	// Both sides of the conflict must be annotated, regardless of which side
	// declared the relation in frontmatter.
	if peers, ok := annotated["01HDDDDDDDDDDDDDDDDDDDDDDD"]; !ok || len(peers) != 1 || peers[0] != "01HEEEEEEEEEEEEEEEEEEEEEEE" {
		t.Errorf("redis side annotation = %v", peers)
	}
	if peers, ok := annotated["01HEEEEEEEEEEEEEEEEEEEEEEE"]; !ok || len(peers) != 1 || peers[0] != "01HDDDDDDDDDDDDDDDDDDDDDDD" {
		t.Errorf("memcached side annotation = %v (mutual conflicts must surface on both sides)", peers)
	}
}

func TestService_ListConflicts_dedupesPair(t *testing.T) {
	svc, db, layer := setupRelationTest(t)
	seedTopic(t, db, layer, "stream-cache-prefer-redis", topicConflictA)
	seedTopic(t, db, layer, "stream-cache-prefer-memcached", topicConflictB)

	pairs, err := svc.ListConflicts()
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 deduplicated pair, got %d: %+v", len(pairs), pairs)
	}
	if pairs[0].Note != "swapped to redis April" {
		t.Errorf("note not preserved: %q", pairs[0].Note)
	}
}

func TestService_Show_outgoingAndIncomingRelations(t *testing.T) {
	svc, db, layer := setupRelationTest(t)
	seedTopic(t, db, layer, "stream-cache-base", topicWithoutRelations)
	seedTopic(t, db, layer, "stream-cache-redux", topicSupersedingBase)
	seedTopic(t, db, layer, "stream-cache-for-sse", topicRefiningBase)

	// The base topic has incoming @supersedes (from B) and incoming @refines
	// (from C). It should show both.
	res, err := svc.Show("stream-cache-base")
	if err != nil {
		t.Fatal(err)
	}
	gotOps := map[string]string{}
	for _, r := range res.Relations {
		gotOps[r.Op] = r.Direction
	}
	if gotOps["@supersedes"] != "in" {
		t.Errorf("expected incoming @supersedes, got %q", gotOps["@supersedes"])
	}
	if gotOps["@refines"] != "in" {
		t.Errorf("expected incoming @refines, got %q", gotOps["@refines"])
	}
	// The other-end summary should be resolved (not dangling).
	for _, r := range res.Relations {
		if r.Other == nil {
			t.Errorf("relation %s/%s has unresolved other end (should be resolvable in our index)", r.Op, r.OtherID)
		}
	}
}

func TestService_TopicList(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{Name: "a", Scope: "personal", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TopicWrite(TopicWriteArgs{Name: "b", Scope: "project:demo", Body: "y"}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.TopicList(TopicListArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("all: got %d, want 2", len(all))
	}
	only, err := svc.TopicList(TopicListArgs{Scope: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 {
		t.Errorf("personal: got %d, want 1", len(only))
	}
}
