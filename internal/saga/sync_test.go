package saga

import (
	"context"
	"errors"
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

func TestPersonalLayerDir(t *testing.T) {
	cfg := &Config{HomeDir: "/some/home/.saga"}
	got := PersonalLayerDir(cfg)
	want := filepath.Join("/some/home/.saga", "personal")
	if got != want {
		t.Errorf("PersonalLayerDir = %q, want %q", got, want)
	}
}
