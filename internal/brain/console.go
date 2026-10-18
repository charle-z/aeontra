package brain

import (
	"context"
	"errors"
)

const (
	MaxConsoleGraphNodes = 500
	MaxConsoleGraphEdges = 1000
)

// ConsoleNode is an opaque graph vertex. IDs are stable HMAC derivations and the
// displayed title/summary come only from the redacted console_metadata table.
type ConsoleNode struct {
	ID           string     `json:"id"`
	ConsoleLabel string     `json:"console_label"`
	Title        string     `json:"title"`
	Summary      string     `json:"summary"`
	Trust        TrustLevel `json:"trust"`
	Degree       int        `json:"degree"`
}

type ConsoleEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type ConsoleSnapshot struct {
	Status         IndexStatus   `json:"status"`
	GraphTruncated bool          `json:"graph_truncated"`
	Nodes          []ConsoleNode `json:"nodes"`
	Edges          []ConsoleEdge `json:"edges"`
}

// ConsoleSnapshot returns bounded aggregate status and a safe opaque link graph.
func (s *Store) ConsoleSnapshot(ctx context.Context) (ConsoleSnapshot, error) {
	if ctx == nil {
		return ConsoleSnapshot{}, errors.New("brain: context is required")
	}
	if s == nil {
		return ConsoleSnapshot{}, errors.New("brain: store is unavailable")
	}
	s.mu.Lock()
	identityReady := len(s.consoleKey) == consoleIdentityBytes
	s.mu.Unlock()
	if !identityReady {
		return ConsoleSnapshot{}, errors.New("brain: console identity is unavailable")
	}
	var snapshot ConsoleSnapshot
	err := s.withIndex(func(index *Index) error {
		queryContext, cancel := boundedContext(ctx, indexQueryTimeout)
		defer cancel()
		status, err := index.status(queryContext)
		if err != nil {
			return err
		}
		snapshot.Status = status

		rows, err := index.db.QueryContext(queryContext, `SELECT n.slug,n.trust,c.console_label,c.title,c.console_summary
			FROM notes n JOIN console_metadata c ON c.slug=n.slug
			ORDER BY n.slug LIMIT ?`, MaxConsoleGraphNodes+1)
		if err != nil {
			return errors.New("brain: console graph query failed")
		}
		defer rows.Close()
		slugs := make([]string, 0, MaxConsoleGraphNodes)
		trustBySlug := make(map[string]TrustLevel, MaxConsoleGraphNodes)
		labelBySlug := make(map[string]string, MaxConsoleGraphNodes)
		titleBySlug := make(map[string]string, MaxConsoleGraphNodes)
		summaryBySlug := make(map[string]string, MaxConsoleGraphNodes)
		for rows.Next() {
			var slug, label, title, summary string
			var trust TrustLevel
			if err := rows.Scan(&slug, &trust, &label, &title, &summary); err != nil {
				return errors.New("brain: console graph result failed")
			}
			if len(slugs) == MaxConsoleGraphNodes {
				snapshot.GraphTruncated = true
				break
			}
			slugs = append(slugs, slug)
			trustBySlug[slug] = trust
			labelBySlug[slug] = label
			titleBySlug[slug] = title
			summaryBySlug[slug] = summary
		}
		if err := rows.Err(); err != nil {
			return errors.New("brain: console graph iteration failed")
		}

		idBySlug := make(map[string]string, len(slugs))
		degree := make(map[string]int, len(slugs))
		for _, slug := range slugs {
			id, err := s.consoleNodeID(slug)
			if err != nil {
				return err
			}
			idBySlug[slug] = id
		}

		edgeRows, err := index.db.QueryContext(queryContext, `SELECT source_slug, target_slug FROM links ORDER BY source_slug, target_slug LIMIT ?`, MaxConsoleGraphEdges+1)
		if err != nil {
			return errors.New("brain: console link query failed")
		}
		defer edgeRows.Close()
		for edgeRows.Next() {
			var sourceSlug, targetSlug string
			if err := edgeRows.Scan(&sourceSlug, &targetSlug); err != nil {
				return errors.New("brain: console link result failed")
			}
			sourceID, sourceOK := idBySlug[sourceSlug]
			targetID, targetOK := idBySlug[targetSlug]
			if !sourceOK || !targetOK {
				snapshot.GraphTruncated = true
				continue
			}
			if len(snapshot.Edges) == MaxConsoleGraphEdges {
				snapshot.GraphTruncated = true
				break
			}
			snapshot.Edges = append(snapshot.Edges, ConsoleEdge{Source: sourceID, Target: targetID})
			degree[sourceSlug]++
			degree[targetSlug]++
		}
		if err := edgeRows.Err(); err != nil {
			return errors.New("brain: console link iteration failed")
		}

		snapshot.Nodes = make([]ConsoleNode, 0, len(slugs))
		for _, slug := range slugs {
			snapshot.Nodes = append(snapshot.Nodes, ConsoleNode{
				ID:           idBySlug[slug],
				ConsoleLabel: labelBySlug[slug],
				Title:        titleBySlug[slug],
				Summary:      summaryBySlug[slug],
				Trust:        trustBySlug[slug],
				Degree:       degree[slug],
			})
		}
		return nil
	})
	return snapshot, err
}
