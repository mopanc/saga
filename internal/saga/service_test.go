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

func TestService_TopicWrite_unknownScopeRejected(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "dept:not-loaded", Body: "y",
	}); err == nil {
		t.Error("expected error on unknown scope")
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
