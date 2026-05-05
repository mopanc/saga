package saga

import (
	"strings"
	"testing"
	"time"
)

func TestLogLembrancas_emptyIDsNoOp(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.LogLembrancas(nil, LembrancaKindHook, "q", "/tmp"); err != nil {
		t.Errorf("expected no-op for empty ids, got error: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}

func TestLogLembrancas_insertsBatch(t *testing.T) {
	svc, db := setupServiceTest(t)

	r1, err := svc.TopicWrite(TopicWriteArgs{
		Name: "topic-a", Scope: "personal", Type: "topic", Body: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.TopicWrite(TopicWriteArgs{
		Name: "topic-b", Scope: "personal", Type: "topic", Body: "b",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.LogLembrancas([]string{r1.ID, r2.ID}, LembrancaKindRecall, "test query", "/some/path"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca WHERE kind = 'recall'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 lembranças, got %d", count)
	}

	// Verify topic_id, query, cwd were stored
	var query, cwd string
	if err := db.QueryRow("SELECT query, cwd FROM lembranca WHERE topic_id = ?", r1.ID).Scan(&query, &cwd); err != nil {
		t.Fatal(err)
	}
	if query != "test query" {
		t.Errorf("query = %q", query)
	}
	if cwd != "/some/path" {
		t.Errorf("cwd = %q", cwd)
	}
}

func TestLogLembrancas_nilQueryAndCwdStoredAsNull(t *testing.T) {
	svc, db := setupServiceTest(t)
	r, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "personal", Type: "profile", Body: "y",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.LogLembrancas([]string{r.ID}, LembrancaKindBaseline, "", ""); err != nil {
		t.Fatal(err)
	}

	var query, cwd interface{}
	if err := db.QueryRow("SELECT query, cwd FROM lembranca WHERE topic_id = ?", r.ID).Scan(&query, &cwd); err != nil {
		t.Fatal(err)
	}
	if query != nil {
		t.Errorf("expected NULL query, got %v", query)
	}
	if cwd != nil {
		t.Errorf("expected NULL cwd, got %v", cwd)
	}
}

func TestLogLembrancas_cascadeDeleteWithTopic(t *testing.T) {
	svc, db := setupServiceTest(t)
	r, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "personal", Type: "topic", Body: "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.LogLembrancas([]string{r.ID}, LembrancaKindHook, "q", "/cwd"); err != nil {
		t.Fatal(err)
	}

	// Delete the topic — lembranças should cascade
	if _, err := db.Exec("DELETE FROM topic_index WHERE id = ?", r.ID); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca WHERE topic_id = ?", r.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected cascade-delete, got %d remaining lembranças", count)
	}
}

func TestLembrancaLog_filtersByKindAndSince(t *testing.T) {
	svc, db := setupServiceTest(t)
	r, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "personal", Type: "topic", Body: "body",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.LogLembrancas([]string{r.ID}, LembrancaKindHook, "q1", "/a"); err != nil {
		t.Fatal(err)
	}
	if err := db.LogLembrancas([]string{r.ID}, LembrancaKindRecall, "q2", "/b"); err != nil {
		t.Fatal(err)
	}

	all, err := svc.LembrancaLog(LembrancaQueryArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("all: got %d, want 2", len(all))
	}

	hookOnly, err := svc.LembrancaLog(LembrancaQueryArgs{Kind: LembrancaKindHook})
	if err != nil {
		t.Fatal(err)
	}
	if len(hookOnly) != 1 {
		t.Errorf("hook-only: got %d, want 1", len(hookOnly))
	}
	if hookOnly[0].Kind != "hook" {
		t.Errorf("kind = %q", hookOnly[0].Kind)
	}

	// Future filter — nothing matches
	future := time.Now().Add(time.Hour).UnixMilli()
	none, err := svc.LembrancaLog(LembrancaQueryArgs{Since: future})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("future: got %d, want 0", len(none))
	}
}

func TestLembrancaLog_filterByTopicTitle(t *testing.T) {
	svc, db := setupServiceTest(t)
	r1, err := svc.TopicWrite(TopicWriteArgs{
		Name: "alpha", Scope: "personal", Type: "topic", Title: "Alpha Topic", Body: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.TopicWrite(TopicWriteArgs{
		Name: "beta", Scope: "personal", Type: "topic", Title: "Beta Topic", Body: "b",
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.LogLembrancas([]string{r1.ID}, LembrancaKindHook, "q", "/x")
	_ = db.LogLembrancas([]string{r2.ID}, LembrancaKindHook, "q", "/x")

	alpha, err := svc.LembrancaLog(LembrancaQueryArgs{Topic: "Alpha Topic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha) != 1 {
		t.Errorf("by title: got %d, want 1", len(alpha))
	}
	if alpha[0].TopicTitle != "Alpha Topic" {
		t.Errorf("topic title = %q", alpha[0].TopicTitle)
	}

	// Filter by topic id
	byID, err := svc.LembrancaLog(LembrancaQueryArgs{Topic: r2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(byID) != 1 || byID[0].TopicID != r2.ID {
		t.Errorf("by id: %+v", byID)
	}
}

func TestRecallRanking_recencyBoostsOlderTopicWithRecentLembranca(t *testing.T) {
	svc, db := setupServiceTest(t)

	rOld, err := svc.TopicWrite(TopicWriteArgs{
		Name: "old", Scope: "personal", Type: "topic", Title: "alpha widget",
		Body: "alpha widget alpha widget alpha widget", // 3x "alpha widget" → high BM25
	})
	if err != nil {
		t.Fatal(err)
	}
	rNew, err := svc.TopicWrite(TopicWriteArgs{
		Name: "new", Scope: "personal", Type: "topic", Title: "alpha note",
		Body: "alpha", // only 1 occurrence — lower BM25
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initially "old" wins by BM25 alone.
	results, err := svc.Recall(RecallArgs{Query: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("expected ≥2 results, got %d", len(results))
	}
	if results[0].ID != rOld.ID {
		t.Logf("first by BM25: %s (this case may be tied; not a hard fail)", results[0].Title)
	}

	// Add a fresh lembrança for "new" — recency boost should outrank BM25.
	if err := db.LogLembrancas([]string{rNew.ID}, LembrancaKindHook, "alpha", "/cwd"); err != nil {
		t.Fatal(err)
	}

	results2, err := svc.Recall(RecallArgs{Query: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if results2[0].ID != rNew.ID {
		t.Errorf("recency boost failed: expected rNew first, got %q (%s)",
			results2[0].Title, results2[0].ID)
	}
}

func TestRecencyWeight(t *testing.T) {
	now := int64(1_700_000_000_000)
	const (
		hour = 60 * 60 * 1000
		day  = 24 * hour
		week = 7 * day
	)
	cases := []struct {
		name string
		last int64
		want float64
	}{
		{"never", 0, 0},
		{"30min", now - 30*60*1000, 1.0},
		{"6h", now - 6*hour, 0.5},
		{"3d", now - 3*day, 0.1},
		{"2w", now - 2*week, 0},
		{"future_clamped", now + hour, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recencyWeight(tc.last, now)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
	// Avoid unused-import warning for strings if not needed elsewhere
	_ = strings.Contains
}
