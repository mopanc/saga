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
	"time"
)

// PersonalLayerDir returns the absolute path of the personal layer root.
func PersonalLayerDir(cfg *Config) string {
	return filepath.Join(cfg.HomeDir, "personal")
}

// SyncOptions controls a Sync run.
type SyncOptions struct {
	PullOnly     bool
	PushOnly     bool
	NoAutoCommit bool
	// DryRun reports what would be pushed and excluded without staging,
	// committing, pulling or pushing. Useful before a real sync.
	DryRun bool
	// CommitMsg overrides the default auto-commit message
	// ("saga: sync <RFC3339>"). Empty string uses the default.
	CommitMsg string
}

// ExcludedTopic describes a topic that sync kept local-only because its
// frontmatter declares `sensitivity: confidential`.
type ExcludedTopic struct {
	ID       string
	Title    string
	FilePath string // relative to layerDir, forward-slashed
}

// SyncResult summarises what Sync did.
type SyncResult struct {
	LayerDir   string
	Remote     string
	Branch     string
	Committed  bool
	Pulled     bool
	Pushed     bool
	PullOutput string
	PushOutput string
	// ExcludedConfidential lists topics filtered out of the push set because
	// their frontmatter declares `sensitivity: confidential`.
	ExcludedConfidential []ExcludedTopic
	// AlreadyPushedWarnings lists confidential topics whose files already
	// exist in origin/<branch> — marking confidential locally does not
	// retroactively remove them from the remote.
	AlreadyPushedWarnings []ExcludedTopic
	// PendingAdds is populated only under DryRun: paths git status reports
	// as pending, after confidential exclusions have been applied.
	PendingAdds []string
}

// ErrNoRemote means the personal layer is not configured as a syncable git repo.
var ErrNoRemote = errors.New("personal layer is not a git repo with a remote configured")

// SyncConflictError signals merge conflicts produced by `pull --rebase`.
type SyncConflictError struct {
	LayerDir string
	Files    []string
}

func (e *SyncConflictError) Error() string {
	return fmt.Sprintf(
		"sync paused: merge conflict in %d file(s):\n  %s\n\nResolve manually:\n  cd %s\n  # edit the files listed above\n  git add <files> && git rebase --continue\n  saga sync --push",
		len(e.Files), strings.Join(e.Files, "\n  "), e.LayerDir,
	)
}

// Sync runs the full pull-then-push dance on the personal layer's git repo.
// It auto-detects the remote via `git config --get remote.origin.url`, optionally
// stages and commits local changes, runs `git pull --rebase --autostash`, then
// `git push`. The caller should reindex after a successful pull (Result.Pulled).
func Sync(ctx context.Context, layerDir string, opts SyncOptions) (*SyncResult, error) {
	res := &SyncResult{LayerDir: layerDir}

	remote, err := readGitRemote(ctx, layerDir)
	if err != nil {
		return nil, ErrNoRemote
	}
	res.Remote = remote

	branch, err := gitOutput(ctx, layerDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read HEAD: %w", err)
	}
	res.Branch = strings.TrimSpace(branch)
	if res.Branch == "HEAD" {
		return nil, fmt.Errorf("personal layer is in a detached HEAD state; checkout a branch first")
	}

	// Identify confidential topics — kept local-only, never staged for push.
	excluded, err := scanConfidentialTopics(layerDir)
	if err != nil {
		return res, fmt.Errorf("scan confidential topics: %w", err)
	}
	res.ExcludedConfidential = excluded
	excludedPaths := make([]string, len(excluded))
	for i, e := range excluded {
		excludedPaths[i] = e.FilePath
	}
	if len(excluded) > 0 {
		res.AlreadyPushedWarnings = alreadyPushedConfidential(ctx, layerDir, res.Branch, excluded)
	}

	if opts.DryRun {
		pending, perr := computePendingAdds(ctx, layerDir, excludedPaths)
		if perr != nil {
			return res, fmt.Errorf("dry-run plan: %w", perr)
		}
		res.PendingAdds = pending
		return res, nil
	}

	if !opts.NoAutoCommit && !opts.PullOnly {
		committed, err := gitAutoCommit(ctx, layerDir, opts.CommitMsg, excludedPaths)
		if err != nil {
			return res, fmt.Errorf("auto-commit: %w", err)
		}
		res.Committed = committed
	}

	if !opts.PushOnly {
		out, err := gitOutput(ctx, layerDir, "pull", "--rebase", "--autostash")
		res.PullOutput = strings.TrimSpace(out)
		res.Pulled = true
		if err != nil {
			if files, _ := unmergedFiles(ctx, layerDir); len(files) > 0 {
				return res, &SyncConflictError{LayerDir: layerDir, Files: files}
			}
			return res, fmt.Errorf("git pull: %w\n%s", err, out)
		}
	}

	if !opts.PullOnly {
		out, err := gitOutput(ctx, layerDir, "push")
		res.PushOutput = strings.TrimSpace(out)
		res.Pushed = true
		if err != nil {
			return res, fmt.Errorf("git push: %w\n%s", err, out)
		}
	}
	return res, nil
}

// SyncStatusReport summarises the layer's sync state without changing anything.
type SyncStatusReport struct {
	LayerDir         string
	HasRemote        bool
	Remote           string
	Branch           string
	UncommittedFiles []string
	AheadBy          int
	BehindBy         int
}

// SyncStatus reports current state without applying changes (no network).
// Ahead/behind counts come from the cached remote-tracking branch and may be
// stale until a `git fetch` is run.
func SyncStatus(ctx context.Context, layerDir string) (*SyncStatusReport, error) {
	rep := &SyncStatusReport{LayerDir: layerDir}

	remote, err := readGitRemote(ctx, layerDir)
	if err != nil {
		return rep, nil
	}
	rep.HasRemote = true
	rep.Remote = remote

	branch, err := gitOutput(ctx, layerDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return rep, fmt.Errorf("read HEAD: %w", err)
	}
	rep.Branch = strings.TrimSpace(branch)

	if out, err := gitOutput(ctx, layerDir, "status", "--porcelain=v1"); err == nil {
		// porcelain v1 format is `XY path` — never strip leading whitespace from
		// individual lines, the X column may legitimately be a single space
		// (means: nothing staged, only unstaged change of type Y).
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if len(line) > 3 {
				rep.UncommittedFiles = append(rep.UncommittedFiles, line[3:])
			}
		}
	}

	if rep.Branch != "" && rep.Branch != "HEAD" {
		out, err := gitOutput(ctx, layerDir, "rev-list", "--left-right", "--count",
			rep.Branch+"...origin/"+rep.Branch)
		if err == nil {
			_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d\t%d", &rep.AheadBy, &rep.BehindBy)
		}
	}
	return rep, nil
}

// --- internal helpers ---

func readGitRemote(ctx context.Context, dir string) (string, error) {
	out, err := gitOutput(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", err
	}
	remote := strings.TrimSpace(out)
	if remote == "" {
		return "", errors.New("remote.origin.url is empty")
	}
	return remote, nil
}

func gitAutoCommit(ctx context.Context, dir, msg string, excluded []string) (bool, error) {
	args := []string{"add", "-A"}
	if len(excluded) > 0 {
		// `git add -A -- . :(exclude)foo :(exclude)bar` stages everything except
		// the named paths. Excluded confidential files stay on disk, untracked
		// (if new) or unstaged (if previously committed — separate `--purge`
		// workflow handles retroactive removal).
		args = append(args, "--", ".")
		for _, p := range excluded {
			args = append(args, ":(exclude)"+p)
		}
	}
	if _, err := gitOutput(ctx, dir, args...); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}
	// `git diff --cached --quiet` exits 0 when nothing staged, 1 when staged.
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--cached", "--quiet") // #nosec G204 -- git is a fixed binary; args static; dir derived from internal config
	if err := cmd.Run(); err == nil {
		return false, nil
	}
	if msg == "" {
		msg = "saga: sync " + time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := gitOutput(ctx, dir, "commit", "-m", msg); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}
	return true, nil
}

// scanConfidentialTopics walks the layer's notes directory and returns topics
// whose frontmatter declares `sensitivity: confidential`. Paths returned are
// relative to layerDir using forward slashes (suitable for git pathspecs).
func scanConfidentialTopics(layerDir string) ([]ExcludedTopic, error) {
	notesDir, err := resolveNotesDir(layerDir)
	if err != nil {
		return nil, err
	}
	var out []ExcludedTopic
	walkErr := filepath.WalkDir(notesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		content, rerr := os.ReadFile(path) // #nosec G304 -- path is rooted at the layer's notes dir resolved from internal config
		if rerr != nil {
			return nil
		}
		topic, perr := ParseTopic(content)
		if perr != nil {
			return nil
		}
		if topic.Sensitivity != "confidential" {
			return nil
		}
		rel, _ := filepath.Rel(layerDir, path)
		out = append(out, ExcludedTopic{
			ID:       topic.ID,
			Title:    topic.Title,
			FilePath: filepath.ToSlash(rel),
		})
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		return nil, walkErr
	}
	return out, nil
}

// resolveNotesDir returns the absolute notes directory for layerDir, defaulting
// to `<layerDir>/topics/` when meta.yml is missing or sets no notes_dir.
func resolveNotesDir(layerDir string) (string, error) {
	layer, err := loadLayer(layerDir)
	if err != nil {
		// No parseable meta.yml — fall back to the convention.
		return filepath.Join(layerDir, "topics"), nil
	}
	return layer.NotesDir, nil
}

// alreadyPushedConfidential reports which excluded confidential paths already
// exist in origin/<branch>. Marking a topic confidential locally does not
// retroactively remove it from the remote — those files require an explicit
// purge workflow (out of scope for v1).
func alreadyPushedConfidential(ctx context.Context, dir, branch string, excluded []ExcludedTopic) []ExcludedTopic {
	if branch == "" || len(excluded) == 0 {
		return nil
	}
	var warnings []ExcludedTopic
	for _, e := range excluded {
		out, err := gitOutput(ctx, dir, "ls-tree", "--name-only", "origin/"+branch, "--", e.FilePath)
		if err != nil {
			continue
		}
		if strings.TrimSpace(out) != "" {
			warnings = append(warnings, e)
		}
	}
	return warnings
}

// computePendingAdds runs `git status --porcelain=v1` and returns paths that
// would be staged by `git add -A`, minus the excluded confidential paths.
// Used by DryRun to render a plan.
func computePendingAdds(ctx context.Context, dir string, excluded []string) ([]string, error) {
	// --untracked-files=all expands untracked directories so each file appears
	// individually; without it git status emits the directory as one line and
	// the excluded pathspec match would miss its children.
	out, err := gitOutput(ctx, dir, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	skip := make(map[string]struct{}, len(excluded))
	for _, p := range excluded {
		skip[p] = struct{}{}
	}
	var pending []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) <= 3 {
			continue
		}
		path := line[3:]
		if _, drop := skip[path]; drop {
			continue
		}
		pending = append(pending, path)
	}
	return pending, nil
}

func unmergedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := gitOutput(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) // #nosec G204 -- git is a fixed binary; args static or from internal layer state; dir from internal config
	out, err := cmd.CombinedOutput()
	return string(out), err
}
