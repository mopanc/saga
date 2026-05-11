package saga

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise Sync against real local git repos (bare remote +
// working clone) — no network, no mocks. Tests skip when git isn't on PATH.

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// setupSyncRepo creates a bare remote and a fresh clone with one initial commit.
// Returns (barePath, workingClonePath).
func setupSyncRepo(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	bareDir := filepath.Join(base, "remote.git")
	workDir := filepath.Join(base, "personal")

	mustRun(t, "", "git", "-c", "init.defaultBranch=main", "init", "--bare", bareDir)
	mustRun(t, "", "git", "-c", "init.defaultBranch=main", "clone", bareDir, workDir)
	mustRun(t, workDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, workDir, "git", "config", "user.name", "saga-test")
	mustRun(t, workDir, "git", "config", "commit.gpgsign", "false")
	mustRun(t, workDir, "git", "config", "tag.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("init\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, workDir, "git", "add", "-A")
	mustRun(t, workDir, "git", "commit", "-m", "init")
	mustRun(t, workDir, "git", "push", "-u", "origin", "HEAD")
	return bareDir, workDir
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func TestSyncErrNoRemoteOnNonRepo(t *testing.T) {
	skipIfNoGit(t)
	if _, err := Sync(context.Background(), t.TempDir(), SyncOptions{}); !errors.Is(err, ErrNoRemote) {
		t.Errorf("got %v, want ErrNoRemote", err)
	}
}

func TestSyncCleanRoundtrip(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	res, err := Sync(context.Background(), work, SyncOptions{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Committed {
		t.Error("expected Committed=false on clean tree")
	}
	if !res.Pulled || !res.Pushed {
		t.Errorf("Pulled=%v Pushed=%v, want both true", res.Pulled, res.Pushed)
	}
	if res.Branch == "" || res.Remote == "" {
		t.Errorf("expected branch+remote populated, got %+v", res)
	}
}

func TestSyncAutoCommitsLocalChanges(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	if err := os.WriteFile(filepath.Join(work, "new.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(context.Background(), work, SyncOptions{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Committed {
		t.Errorf("expected Committed=true after creating new.md")
	}
}

func TestSyncNoAutoCommitLeavesWorkdirDirty(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	if err := os.WriteFile(filepath.Join(work, "new.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(context.Background(), work, SyncOptions{NoAutoCommit: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Committed {
		t.Error("expected Committed=false with NoAutoCommit")
	}
}

func TestSyncPullOnlySkipsPushAndAutoCommit(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	if err := os.WriteFile(filepath.Join(work, "uncommitted.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(context.Background(), work, SyncOptions{PullOnly: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Committed {
		t.Error("PullOnly must not auto-commit")
	}
	if !res.Pulled {
		t.Error("PullOnly should pull")
	}
	if res.Pushed {
		t.Error("PullOnly must not push")
	}
}

func TestSyncPushOnlySkipsPull(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	if err := os.WriteFile(filepath.Join(work, "new.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(context.Background(), work, SyncOptions{PushOnly: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Pulled {
		t.Error("PushOnly must not pull")
	}
	if !res.Committed || !res.Pushed {
		t.Errorf("expected Committed && Pushed, got %+v", res)
	}
}

func TestSyncConflictReturnsTypedError(t *testing.T) {
	skipIfNoGit(t)
	bare, work := setupSyncRepo(t)

	// Second clone publishes a divergent change to README.md
	other := t.TempDir()
	mustRun(t, "", "git", "clone", bare, other)
	mustRun(t, other, "git", "config", "user.email", "test@example.com")
	mustRun(t, other, "git", "config", "user.name", "saga-test")
	mustRun(t, other, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("from-other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, other, "git", "add", "-A")
	mustRun(t, other, "git", "commit", "-m", "from other")
	mustRun(t, other, "git", "push")

	// work makes a conflicting change to the same file
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("from-work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Sync(context.Background(), work, SyncOptions{})
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	var conflict *SyncConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *SyncConflictError, got %T: %v", err, err)
	}
	if len(conflict.Files) == 0 {
		t.Error("expected conflict.Files to be populated")
	}
	// Cleanup: abort the rebase the test left in flight
	mustRun(t, work, "git", "rebase", "--abort")
}

func TestSyncStatusOnNonRepo(t *testing.T) {
	skipIfNoGit(t)
	rep, err := SyncStatus(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}
	if rep.HasRemote {
		t.Error("HasRemote must be false on non-repo")
	}
}

func TestSyncStatusReportsRemoteAndBranch(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	rep, err := SyncStatus(context.Background(), work)
	if err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}
	if !rep.HasRemote {
		t.Error("HasRemote must be true")
	}
	if rep.Branch == "" {
		t.Error("Branch must be set")
	}
	if !strings.Contains(rep.Remote, "remote.git") {
		t.Errorf("Remote = %q, want path containing 'remote.git'", rep.Remote)
	}
}

func TestSyncStatusPreservesFilenamesWithLeadingPorcelainSpace(t *testing.T) {
	// Regression: an earlier version did strings.TrimSpace on the entire
	// `git status --porcelain` blob, which ate the leading space of the
	// first line — causing files to be reported as "opics/foo.md" instead
	// of "topics/foo.md" when the first entry's status code was " M".
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := SyncStatus(context.Background(), work)
	if err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}
	if len(rep.UncommittedFiles) != 1 {
		t.Fatalf("expected 1 uncommitted file, got %d: %v", len(rep.UncommittedFiles), rep.UncommittedFiles)
	}
	if rep.UncommittedFiles[0] != "README.md" {
		t.Errorf("UncommittedFiles[0] = %q, want %q", rep.UncommittedFiles[0], "README.md")
	}
}

// writeTopic creates a minimal-but-valid topic file in <workDir>/topics/<name>.md
// with the given sensitivity. id is derived from name so callers can assert it.
func writeTopic(t *testing.T, workDir, name, sensitivity string) string {
	t.Helper()
	topicsDir := filepath.Join(workDir, "topics")
	if err := os.MkdirAll(topicsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := "01J" + strings.ToUpper(name) + "ZZZ"
	body := fmt.Sprintf(`---
id: %s
scope: personal
type: topic
title: %s
sensitivity: %s
confidence: proposed
created_at: 2026-01-01T00:00:00Z
updated_at: 2026-01-01T00:00:00Z
---

body of %s
`, id, name, sensitivity, name)
	path := filepath.Join(topicsDir, name+".md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return id
}

// writeMeta drops a minimal meta.yml so loadLayer recognises the dir.
func writeMeta(t *testing.T, workDir string) {
	t.Helper()
	meta := []byte("scope: personal\ndisplay_name: test\nnotes_dir: topics/\n")
	if err := os.WriteFile(filepath.Join(workDir, "meta.yml"), meta, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSyncExcludesConfidentialTopicsFromPush(t *testing.T) {
	skipIfNoGit(t)
	bare, work := setupSyncRepo(t)
	writeMeta(t, work)
	writeTopic(t, work, "public", "internal")
	writeTopic(t, work, "secret", "confidential")

	res, err := Sync(context.Background(), work, SyncOptions{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !res.Committed || !res.Pushed {
		t.Fatalf("expected Committed && Pushed, got %+v", res)
	}
	if len(res.ExcludedConfidential) != 1 {
		t.Fatalf("ExcludedConfidential len=%d, want 1: %+v", len(res.ExcludedConfidential), res.ExcludedConfidential)
	}
	if got := res.ExcludedConfidential[0].FilePath; got != "topics/secret.md" {
		t.Errorf("ExcludedConfidential path = %q, want %q", got, "topics/secret.md")
	}

	// Verify on the remote: secret.md must NOT exist; public.md must.
	other := t.TempDir()
	mustRun(t, "", "git", "clone", bare, other)
	if _, err := os.Stat(filepath.Join(other, "topics", "public.md")); err != nil {
		t.Errorf("expected public.md in remote clone, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "topics", "secret.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected secret.md NOT in remote clone, stat err = %v", err)
	}
}

func TestSyncConfidentialFileStaysLocal(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	writeMeta(t, work)
	writeTopic(t, work, "secret", "confidential")

	if _, err := Sync(context.Background(), work, SyncOptions{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "topics", "secret.md")); err != nil {
		t.Errorf("confidential file must remain on disk; stat err = %v", err)
	}
}

func TestSyncDryRunListsPendingAndExcluded(t *testing.T) {
	skipIfNoGit(t)
	bare, work := setupSyncRepo(t)
	writeMeta(t, work)
	writeTopic(t, work, "public", "internal")
	writeTopic(t, work, "secret", "confidential")

	res, err := Sync(context.Background(), work, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync dry-run: %v", err)
	}
	if res.Committed || res.Pulled || res.Pushed {
		t.Errorf("dry-run must not mutate: %+v", res)
	}
	if len(res.ExcludedConfidential) != 1 {
		t.Errorf("ExcludedConfidential len=%d, want 1", len(res.ExcludedConfidential))
	}
	wantAdds := map[string]bool{"meta.yml": true, "topics/public.md": true}
	for _, p := range res.PendingAdds {
		delete(wantAdds, p)
	}
	if len(wantAdds) != 0 {
		t.Errorf("PendingAdds missing entries %v; got %v", wantAdds, res.PendingAdds)
	}
	for _, p := range res.PendingAdds {
		if p == "topics/secret.md" {
			t.Errorf("PendingAdds must not include confidential topic; got %v", res.PendingAdds)
		}
	}

	// Confirm nothing was actually pushed.
	other := t.TempDir()
	mustRun(t, "", "git", "clone", bare, other)
	if _, err := os.Stat(filepath.Join(other, "topics", "public.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("dry-run must not push: public.md should NOT exist in remote, stat err = %v", err)
	}
}

func TestSyncWarnsWhenConfidentialAlreadyInRemote(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	writeMeta(t, work)
	// Write as internal first, push it.
	writeTopic(t, work, "regret", "internal")
	if _, err := Sync(context.Background(), work, SyncOptions{}); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	// Flip to confidential and sync again.
	writeTopic(t, work, "regret", "confidential")
	res, err := Sync(context.Background(), work, SyncOptions{})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(res.AlreadyPushedWarnings) != 1 {
		t.Fatalf("AlreadyPushedWarnings len=%d, want 1: %+v", len(res.AlreadyPushedWarnings), res.AlreadyPushedWarnings)
	}
	if got := res.AlreadyPushedWarnings[0].FilePath; got != "topics/regret.md" {
		t.Errorf("AlreadyPushedWarnings path = %q, want %q", got, "topics/regret.md")
	}
}

func TestSyncNoWarningWhenConfidentialNeverPushed(t *testing.T) {
	skipIfNoGit(t)
	_, work := setupSyncRepo(t)
	writeMeta(t, work)
	writeTopic(t, work, "secret", "confidential")
	res, err := Sync(context.Background(), work, SyncOptions{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.AlreadyPushedWarnings) != 0 {
		t.Errorf("AlreadyPushedWarnings must be empty for never-pushed confidential, got %+v", res.AlreadyPushedWarnings)
	}
	if len(res.ExcludedConfidential) != 1 {
		t.Errorf("ExcludedConfidential must still report exclusion, got %+v", res.ExcludedConfidential)
	}
}

func TestPersonalLayerDir(t *testing.T) {
	cfg := &Config{HomeDir: "/some/home/.saga"}
	got := PersonalLayerDir(cfg)
	want := filepath.Join("/some/home/.saga", "personal")
	if got != want {
		t.Errorf("PersonalLayerDir = %q, want %q", got, want)
	}
}
