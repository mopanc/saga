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

type parsedNote struct {
	path    string
	content []byte
	topic   *Topic
}

// IndexLayer rebuilds a layer's index entries from its notes directory in three
// phases: parse every note (no writes), prune index rows whose id is no longer
// on disk, then persist (upsert) the parsed notes. Files that fail to parse are
// recorded in Errors but don't abort the run.
//
// Two ordering decisions matter:
//
//   - We do NOT wipe the layer up front. The old wipe (DELETE FROM topic_index
//     WHERE source_layer) fired ON DELETE CASCADE and destroyed the lembrança
//     usage history of every topic on every reindex. Upserting by id keeps the
//     row for topics that still exist. Since migration 005 lembrança no longer
//     hangs off that FK at all, so history survives the prune too — but the
//     upsert is still what keeps a reindex from churning every row.
//
//   - We prune BEFORE persisting, not after. A note that keeps its title but
//     changes id (a reorg shuffle) would otherwise collide with its own stale
//     row on UNIQUE(scope,title) during insert — and that row would then be
//     pruned, making the note vanish. Pruning first frees the title. Genuine
//     duplicates (two files sharing scope+title) still surface as a per-file
//     UNIQUE error, which is correct.
func (db *DB) IndexLayer(layer Layer) (*IndexResult, error) {
	result := &IndexResult{LayerScope: layer.Scope}

	var notes []parsedNote
	seen := make(map[string]struct{})

	if _, err := os.Stat(layer.NotesDir); !errors.Is(err, fs.ErrNotExist) {
		walkErr := filepath.WalkDir(layer.NotesDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
				return nil
			}
			topic, content, err := db.parseFile(path, layer)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, IndexError{File: path, Err: err})
				return nil
			}
			notes = append(notes, parsedNote{path: path, content: content, topic: topic})
			seen[topic.ID] = struct{}{}
			return nil
		})
		if walkErr != nil {
			return result, fmt.Errorf("walk %s: %w", layer.NotesDir, walkErr)
		}
	}

	if err := db.pruneUnseenTopics(layer.Scope, seen); err != nil {
		return result, err
	}

	for _, n := range notes {
		if err := db.persistTopic(n.topic, n.content, n.path, layer.Scope); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, IndexError{File: n.path, Err: err})
			continue
		}
		result.Indexed++
	}
	return result, nil
}

// pruneUnseenTopics removes index rows for a layer whose topic id was not seen
// during the current indexing pass — i.e. notes deleted from disk, or moved to
// another layer. Removing a topic cascades to its topic_reference and
// topic_relation rows (both derived from the note, rebuilt on next index);
// topic_fts has no cascade, so it is cleared explicitly.
//
// It does NOT touch lembranca. Since migration 005 that table has no FK to
// topic_index, precisely because this prune cannot tell "note deleted" from
// "note moved to another layer" — the two look identical from inside one
// layer's pass, and cascading destroyed the history of every moved note (#95).
// Orphaned lembranças are retained as history and reclaimed by `saga gc`.
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

// parseFile reads and parses a single .md file, validating that its declared
// scope matches the layer. It performs no database writes.
func (db *DB) parseFile(path string, layer Layer) (*Topic, []byte, error) {
	content, err := os.ReadFile(path) // #nosec G304 -- path is from filepath.WalkDir over the layer's NotesDir
	if err != nil {
		return nil, nil, fmt.Errorf("read: %w", err)
	}
	topic, err := ParseTopic(content)
	if err != nil {
		return nil, nil, fmt.Errorf("parse: %w", err)
	}
	// Defend against scope drift: the file's frontmatter scope must match
	// the layer it lives in. Otherwise the index lies about provenance.
	if topic.Scope != layer.Scope {
		return nil, nil, fmt.Errorf("scope mismatch: file declares %q, layer is %q", topic.Scope, layer.Scope)
	}
	return topic, content, nil
}

// persistTopic upserts one parsed topic — plus its FTS row, references and
// relations — into the index in a single transaction. layerScope is stored as
// source_layer.
func (db *DB) persistTopic(topic *Topic, content []byte, path, layerScope string) error {
	hash := sha256Hex(content)
	// jsonList, not json.Marshal: a nil slice marshals to "null", so an absent
	// list and an empty one were stored differently. Queries then read as
	// obviously-correct while being wrong — `WHERE triggers != '[]'` matched
	// every note that simply had none.
	synJSON, err := jsonList(topic.Synonyms)
	if err != nil {
		return err
	}
	trigJSON, err := jsonList(topic.Triggers)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`
		INSERT INTO topic_index (
			id, scope, type, title, synonyms, sensitivity, confidence,
			file_path, file_hash, source_layer, created_at, updated_at,
			triggers, enforcement
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			updated_at   = excluded.updated_at,
			triggers     = excluded.triggers,
			enforcement  = excluded.enforcement
	`,
		topic.ID, topic.Scope, topic.Type, topic.Title, synJSON,
		nonEmpty(topic.Sensitivity, "internal"),
		nonEmpty(topic.Confidence, "proposed"),
		path, hash, layerScope,
		topic.CreatedAt.UnixMilli(), topic.UpdatedAt.UnixMilli(),
		trigJSON, nonEmpty(topic.Enforcement, "advise"),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("upsert topic_index: %w", err)
	}

	// FTS5 has no upsert; delete then insert.
	if _, err := tx.Exec("DELETE FROM topic_fts WHERE id = ?", topic.ID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO topic_fts (id, scope, title, synonyms, body)
		VALUES (?, ?, ?, ?, ?)
	`, topic.ID, topic.Scope, topic.Title, strings.Join(topic.Synonyms, " "), topic.Body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert topic_fts: %w", err)
	}

	// Replace references (cascade-deleted if topic existed; here we re-insert).
	if _, err := tx.Exec("DELETE FROM topic_reference WHERE topic_id = ?", topic.ID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, ref := range topic.References {
		if _, err := tx.Exec(`
			INSERT INTO topic_reference (topic_id, path, lines, blame_hash, is_stale)
			VALUES (?, ?, ?, ?, 0)
		`, topic.ID, ref.Path, ref.Lines, ref.BlameHash); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert topic_reference: %w", err)
		}
	}

	// Replace relations. Same upsert-by-replace pattern as references.
	if _, err := tx.Exec("DELETE FROM topic_relation WHERE source_id = ?", topic.ID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, rel := range topic.Relations {
		if _, err := tx.Exec(`
			INSERT INTO topic_relation (source_id, op, target_id, note)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(source_id, op, target_id) DO UPDATE SET note = excluded.note
		`, topic.ID, rel.Op, rel.Target, nullableString(rel.Note)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert topic_relation: %w", err)
		}
	}

	return tx.Commit()
}

// indexFile parses and persists a single .md file, returning the topic id. Used
// by TopicWrite (single write after each edit). IndexLayer instead calls
// parseFile and persistTopic directly so it can prune stale rows between the
// two phases.
func (db *DB) indexFile(path string, layer Layer) (string, error) {
	topic, content, err := db.parseFile(path, layer)
	if err != nil {
		return "", err
	}
	if err := db.persistTopic(topic, content, path, layer.Scope); err != nil {
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

// jsonList marshals a string slice, rendering nil as an empty JSON array rather
// than "null" so the column has one representation for "no entries".
func jsonList(v []string) (string, error) {
	if v == nil {
		v = []string{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
