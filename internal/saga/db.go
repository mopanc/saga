package saga

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	*sql.DB
}

func OpenDB(path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("mkdir db parent: %w", err)
		}
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db := &DB{sqlDB}
	if err := db.applyMigrations(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return db, nil
}

func (db *DB) applyMigrations() error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		version    TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return err
	}

	applied := map[string]bool{}
	rows, err := db.Query("SELECT version FROM _migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return err
		}
		applied[v] = true
	}
	_ = rows.Close()

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		version := strings.TrimSuffix(f, ".sql")
		if applied[version] {
			continue
		}
		sqlBytes, err := fs.ReadFile(migrationsFS, "migrations/"+f)
		if err != nil {
			return err
		}
		if err := db.applyMigration(version, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one migration script in its own transaction, on a pinned
// connection with foreign key enforcement disabled.
//
// Schema-changing migrations must rebuild the table (SQLite has no ALTER/DROP
// CONSTRAINT): create-new, copy, DROP old, rename. With foreign keys ON, that
// DROP fires ON DELETE CASCADE on child tables — silently wiping `lembranca`
// (the usage-history "gold") and `topic_relation`. PRAGMA defer_foreign_keys
// only defers constraint *checking* to COMMIT; it does NOT suppress cascade
// *actions*, which is why the earlier attempt failed. The correct fix is
// PRAGMA foreign_keys=OFF — but that is a no-op inside a transaction, so it
// must be set on the connection before BEGIN and restored afterwards, guarded
// by an integrity check.
func (db *DB) applyMigration(version, script string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return fmt.Errorf("migration %s: disable foreign_keys: %w", version, err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, script); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO _migrations (version, applied_at) VALUES (?, CAST(strftime('%s','now') AS INTEGER) * 1000)",
		version,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// A rebuilt table must not leave dangling child rows. foreign_key_check
	// returns one row per violation; any row means the migration corrupted
	// referential integrity and must not be trusted.
	if err := foreignKeyCheck(ctx, conn); err != nil {
		return fmt.Errorf("migration %s: %w", version, err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("migration %s: re-enable foreign_keys: %w", version, err)
	}
	return nil
}

// foreignKeyCheck runs PRAGMA foreign_key_check on conn and returns an error if
// any referential-integrity violation is reported.
func foreignKeyCheck(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return errors.New("foreign key check failed: dangling references after migration")
	}
	return rows.Err()
}
