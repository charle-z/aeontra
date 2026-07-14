package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultContextNotes      = 8
	MaxContextNotes          = 16
	MaxContextBytes          = 4 << 10
	MaxContextProvenanceByte = 192
)

// ContextDigest returns a bounded on-demand session digest. It deliberately omits
// note bodies and prefers curated notes before recent, non-expired working notes.
func (s *Store) ContextDigest(ctx context.Context, limit int) (string, error) {
	if ctx == nil {
		return "", errors.New("brain: context is required")
	}
	if limit == 0 {
		limit = DefaultContextNotes
	}
	if limit < 1 || limit > MaxContextNotes {
		return "", fmt.Errorf("brain: context limit must be between 1 and %d", MaxContextNotes)
	}

	var digest strings.Builder
	err := s.withIndex(func(index *Index) error {
		queryContext, cancel := boundedContext(ctx, indexQueryTimeout)
		defer cancel()
		rows, err := index.db.QueryContext(queryContext, `SELECT
			slug, trust, note_type, title, author, review_by, provenance
		FROM notes
		WHERE trust='curated' OR expired=0
		ORDER BY CASE trust WHEN 'curated' THEN 0 ELSE 1 END, updated DESC, slug ASC
		LIMIT ?`, limit)
		if err != nil {
			return errors.New("brain: SQLite context query failed")
		}
		defer rows.Close()
		for rows.Next() {
			var slug, trust, noteType, title, author, reviewBy, provenance string
			if err := rows.Scan(&slug, &trust, &noteType, &title, &author, &reviewBy, &provenance); err != nil {
				return errors.New("brain: SQLite context result failed")
			}
			title = contextField(title, MaxTitleBytes)
			provenance = contextField(provenance, MaxContextProvenanceByte)
			line := fmt.Sprintf("%s | %s | %s | %s | %s", slug, trust, noteType, title, author)
			if reviewBy != "" {
				line += " | review_by=" + reviewBy
			}
			line += " | provenance=" + provenance + "\n"
			if digest.Len()+len(line) > MaxContextBytes {
				break
			}
			digest.WriteString(line)
		}
		if err := rows.Err(); err != nil {
			return errors.New("brain: SQLite context iteration failed")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(digest.String(), "\n"), nil
}

func contextField(value string, maximum int) string {
	value = strings.ReplaceAll(value, "|", "/")
	return truncateUTF8(strings.TrimSpace(value), maximum)
}
