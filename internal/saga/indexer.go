package saga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IndexResult — summary of an IndexLayer call.
type IndexResult struct {
	LayerScope string
	Indexed    int
	Failed     int
	Errors     []IndexError
}

type IndexError struct {
	File string
	Err  error
}

// IndexLayer rebuilds a layer's index entries from its notes directory. It
// upserts every note found (by id), then prunes only the index rows whose .md
// file no longer exists on disk. Files that fail to parse are recorded in
// Errors but don't abort the walk — partial indexing is preferred over an
// empty index.
//
// Note the ordering: we deliberately do NOT wipe the layer before indexing.
// The old wipe (DELETE FROM topic_index WHERE source_layer) fired ON DELETE
// CASCADE and destroyed the lembrança usage history of every topic on every
// reindex. Upserting first keeps the row (and its id) for topics that still
// exist, so their history survives; only genuinely-removed topics are pruned.
func (db *DB) IndexLayer(layer Layer) (*IndexResult, error) {
	result := &IndexResult{LayerScope: layer.Scope}

	seen := make(map[string]struct{})

	if _, err := os.Stat(layer.NotesDir); !errors.Is(err, fs.ErrNotExist) {
		walkErr := filepath.WalkDir(layer.NotesDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
				return nil
			}
			id, err := db.indexFile(path, layer)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, IndexError{File: path, Err: err})
				return nil
			}
			seen[id] = struct{}{}
			result.Indexed++
			return nil
		})
		if walkErr != nil {
			return result, fmt.Errorf("walk %s: %w", layer.NotesDir, walkErr)
		}
	}

	if err := db.pruneUnseenTopics(layer.Scope, seen); err != nil {
		return result, err
	}
	return result, nil
}

// pruneUnseenTopics removes index rows for a layer whose topic id was not seen
// during the current indexing pass — i.e. notes deleted from disk. Removing a
// topic cascades to its topic_reference, topic_relation and lembranca rows
// (the note is genuinely gone); topic_fts has no cascade, so it is cleared
// explicitly. Whether usage history should outlive its note is a separate
// design decision (see the ON DELETE CASCADE on lembranca.topic_id).
func (db *DB) pruneUnseenTopics(scope string, seen map[string]struct{}) error {
	rows, err := db.Query("SELECT id FROM topic_index WHERE source_layer = ?", scope)
	if err != nil {
		return fmt.Errorf("list layer topics: %w", err)
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		if _, ok := seen[id]; !ok {
			stale = append(stale, id)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range stale {
		if _, err := db.Exec("DELETE FROM topic_index WHERE id = ?", id); err != nil {
			return fmt.Errorf("prune topic_index %s: %w", id, err)
		}
		if _, err := db.Exec("DELETE FROM topic_fts WHERE id = ?", id); err != nil {
			return fmt.Errorf("prune topic_fts %s: %w", id, err)
		}
	}
	return nil
}

// indexFile parses a single .md file and upserts it into the index. It returns
// the topic id on success so callers (IndexLayer) can track which topics were
// seen this pass. Used both by IndexLayer (bulk) and TopicWrite (single).
func (db *DB) indexFile(path string, layer Layer) (string, error) {
	content, err := os.ReadFile(path) // #nosec G304 -- path is from filepath.WalkDir over the layer's NotesDir
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	topic, err := ParseTopic(content)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	// Defend against scope drift: the file's frontmatter scope must match
	// the layer it lives in. Otherwise the index lies about provenance.
	if topic.Scope != layer.Scope {
		return "", fmt.Errorf("scope mismatch: file declares %q, layer is %q", topic.Scope, layer.Scope)
	}

	hash := sha256Hex(content)
	synJSON, err := json.Marshal(topic.Synonyms)
	if err != nil {
		return "", err
	}

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(
		`
		INSERT INTO topic_index (
			id, scope, type, title, synonyms, sensitivity, confidence,
			file_path, file_hash, source_layer, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			scope        = excluded.scope,
			type         = excluded.type,
			title        = excluded.title,
			synonyms     = excluded.synonyms,
			sensitivity  = excluded.sensitivity,
			confidence   = excluded.confidence,
			file_path    = excluded.file_path,
			file_hash    = excluded.file_hash,
			source_layer = excluded.source_layer,
			updated_at   = excluded.updated_at
	`,
		topic.ID, topic.Scope, topic.Type, topic.Title, string(synJSON),
		nonEmpty(topic.Sensitivity, "internal"),
		nonEmpty(topic.Confidence, "proposed"),
		path, hash, layer.Scope,
		topic.CreatedAt.UnixMilli(), topic.UpdatedAt.UnixMilli(),
	); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("upsert topic_index: %w", err)
	}

	// FTS5 has no upsert; delete then insert.
	if _, err := tx.Exec("DELETE FROM topic_fts WHERE id = ?", topic.ID); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if _, err := tx.Exec(`
		INSERT INTO topic_fts (id, scope, title, synonyms, body)
		VALUES (?, ?, ?, ?, ?)
	`, topic.ID, topic.Scope, topic.Title, strings.Join(topic.Synonyms, " "), topic.Body); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("insert topic_fts: %w", err)
	}

	// Replace references (cascade-deleted if topic existed; here we re-insert).
	if _, err := tx.Exec("DELETE FROM topic_reference WHERE topic_id = ?", topic.ID); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	for _, ref := range topic.References {
		if _, err := tx.Exec(`
			INSERT INTO topic_reference (topic_id, path, lines, blame_hash, is_stale)
			VALUES (?, ?, ?, ?, 0)
		`, topic.ID, ref.Path, ref.Lines, ref.BlameHash); err != nil {
			_ = tx.Rollback()
			return "", fmt.Errorf("insert topic_reference: %w", err)
		}
	}

	// Replace relations. Same upsert-by-replace pattern as references.
	if _, err := tx.Exec("DELETE FROM topic_relation WHERE source_id = ?", topic.ID); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	for _, rel := range topic.Relations {
		if _, err := tx.Exec(`
			INSERT INTO topic_relation (source_id, op, target_id, note)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(source_id, op, target_id) DO UPDATE SET note = excluded.note
		`, topic.ID, rel.Op, rel.Target, nullableString(rel.Note)); err != nil {
			_ = tx.Rollback()
			return "", fmt.Errorf("insert topic_relation: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return topic.ID, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
