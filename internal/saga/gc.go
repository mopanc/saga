package saga

import "fmt"

// OrphanGroup is the usage history of one topic id that is not currently in
// topic_index.
type OrphanGroup struct {
	TopicID     string
	Count       int
	LastTrigger int64 // unix ms of the most recent lembrança
}

// OrphanedLembrancas groups lembrança rows whose topic_id has no row in
// topic_index, most history first.
//
// Since migration 005 these are retained rather than cascade-deleted, because
// the indexer cannot distinguish a deleted note from one moved to another layer
// (#95). That same ambiguity applies here: an "orphan" is either history of a
// genuinely deleted note, or history of a note that lives in a project layer
// not active in the current working directory. Callers must not treat this list
// as garbage without that caveat — see PruneOrphanedLembrancas.
func (db *DB) OrphanedLembrancas() ([]OrphanGroup, error) {
	rows, err := db.Query(`
		SELECT l.topic_id, COUNT(*), MAX(l.triggered_at)
		FROM lembranca l
		WHERE NOT EXISTS (SELECT 1 FROM topic_index t WHERE t.id = l.topic_id)
		GROUP BY l.topic_id
		ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query orphaned lembranças: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []OrphanGroup
	for rows.Next() {
		var g OrphanGroup
		if err := rows.Scan(&g.TopicID, &g.Count, &g.LastTrigger); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PruneOrphanedLembrancas deletes the history of the given topic ids and
// returns the number of rows removed. It takes explicit ids rather than
// deleting every orphan, so a caller can never wipe the history of a note that
// is merely filed in an inactive layer.
//
// This is irreversible: lembranças are not reconstructible from the notes on
// disk.
func (db *DB) PruneOrphanedLembrancas(topicIDs []string) (int, error) {
	total := 0
	for _, id := range topicIDs {
		res, err := db.Exec(`
			DELETE FROM lembranca
			WHERE topic_id = ?
			  AND NOT EXISTS (SELECT 1 FROM topic_index t WHERE t.id = lembranca.topic_id)
		`, id)
		if err != nil {
			return total, fmt.Errorf("prune lembranças for %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += int(n)
	}
	return total, nil
}
