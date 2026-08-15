package saga

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// VaultLink is one layer as it appears inside the vault.
type VaultLink struct {
	Scope    string
	Target   string // the layer's real notes directory
	LinkPath string // where the vault points at it from
	Notes    int
	Created  bool // false when the link was already correct
}

// VaultResult is the outcome of building a vault.
type VaultResult struct {
	Root  string
	Links []VaultLink
}

// BuildVault assembles a directory of symlinks, one per layer, so every layer's
// notes can be opened as a single Obsidian vault: one graph across personal and
// every project, rather than a graph per repository.
//
// Symlinks rather than copies: the notes stay where they are, saga remains the
// only writer of record, and editing a note in Obsidian edits the real file. A
// copy would fork the truth the moment either side changed.
//
// Layers are discovered from the index rather than from the working directory,
// because the point is to see everything at once — the cwd-scoped resolver
// would only ever surface the layers active where the command happened to run.
//
// Refuses to replace anything that is not already a symlink. A vault path that
// holds a real directory is either a mistake or someone's data; either way,
// silently removing it to make room is not this command's call.
func (s *Service) BuildVault(root string) (*VaultResult, error) {
	rows, err := s.db.Query(`
		SELECT source_layer, file_path, COUNT(*)
		FROM topic_index
		GROUP BY source_layer, file_path
		ORDER BY source_layer
	`)
	if err != nil {
		return nil, fmt.Errorf("query layers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	dirs := map[string]string{}
	counts := map[string]int{}
	for rows.Next() {
		var scope, path string
		var n int
		if err := rows.Scan(&scope, &path, &n); err != nil {
			return nil, err
		}
		dirs[scope] = filepath.Dir(path)
		counts[scope] += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		return &VaultResult{Root: root}, nil
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}

	scopes := make([]string, 0, len(dirs))
	for sc := range dirs {
		scopes = append(scopes, sc)
	}
	sort.Strings(scopes)

	result := &VaultResult{Root: root}
	for _, scope := range scopes {
		target := dirs[scope]
		if _, err := os.Stat(target); err != nil {
			continue // layer recorded in the index but gone from disk
		}
		link := filepath.Join(root, vaultLinkName(scope))

		created, err := ensureSymlink(target, link)
		if err != nil {
			return nil, err
		}
		result.Links = append(result.Links, VaultLink{
			Scope:    scope,
			Target:   target,
			LinkPath: link,
			Notes:    counts[scope],
			Created:  created,
		})
	}
	return result, nil
}

// ensureSymlink points link at target, reporting whether it had to change
// anything. Returns an error rather than replacing a non-symlink.
func ensureSymlink(target, link string) (bool, error) {
	info, err := os.Lstat(link)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		current, rerr := os.Readlink(link)
		if rerr == nil && current == target {
			return false, nil
		}
		if rerr := os.Remove(link); rerr != nil {
			return false, fmt.Errorf("replace stale link %s: %w", link, rerr)
		}
	case err == nil:
		return false, fmt.Errorf("%s exists and is not a symlink; move it aside first "+
			"(saga will not delete real files to build a vault)", link)
	case !os.IsNotExist(err):
		return false, err
	}

	if err := os.Symlink(target, link); err != nil {
		return false, fmt.Errorf("link %s -> %s: %w", link, target, err)
	}
	return true, nil
}

// vaultLinkName turns a scope into a directory name that reads well in
// Obsidian's file tree: "project:qscope-v3" becomes "qscope-v3", "personal"
// stays "personal". Colons are legal on the filesystems saga targets but make
// for awkward paths, and the prefix carries no information once each layer is
// its own folder.
func vaultLinkName(scope string) string {
	name := strings.TrimPrefix(scope, "project:")
	name = strings.ReplaceAll(name, ":", "-")
	if name == "" {
		return "unnamed"
	}
	return name
}
