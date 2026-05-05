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

// IndexLayer wipes the index entries for a layer and rebuilds them from the
// layer's notes directory. Files that fail to parse are recorded in Errors
// but don't abort the walk — partial indexing is preferred over an empty index.
func (db *DB) IndexLayer(layer Layer) (*IndexResult, error) {
	result := &IndexResult{LayerScope: layer.Scope}

	// Wipe existing entries for this layer (cascade deletes topic_reference).
	if _, err := db.Exec("DELETE FROM topic_index WHERE source_layer = ?", layer.Scope); err != nil {
		return nil, fmt.Errorf("wipe topic_index: %w", err)
	}
	if _, err := db.Exec("DELETE FROM topic_fts WHERE scope = ?", layer.Scope); err != nil {
		return nil, fmt.Errorf("wipe topic_fts: %w", err)
	}

	if _, err := os.Stat(layer.NotesDir); errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}

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
		if err := db.indexFile(path, layer); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, IndexError{File: path, Err: err})
			return nil
		}
		result.Indexed++
		return nil
	})
	if walkErr != nil {
		return result, fmt.Errorf("walk %s: %w", layer.NotesDir, walkErr)
	}
	return result, nil
}

// indexFile parses a single .md file and upserts it into the index.
// Used both by IndexLayer (bulk) and TopicWrite (single, after each write).
func (db *DB) indexFile(path string, layer Layer) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	topic, err := ParseTopic(content)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Defend against scope drift: the file's frontmatter scope must match
	// the layer it lives in. Otherwise the index lies about provenance.
	if topic.Scope != layer.Scope {
		return fmt.Errorf("scope mismatch: file declares %q, layer is %q", topic.Scope, layer.Scope)
	}

	hash := sha256Hex(content)
	synJSON, err := json.Marshal(topic.Synonyms)
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

	return tx.Commit()
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
