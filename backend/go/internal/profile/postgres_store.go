package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the pgx-backed implementation of ActivityStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ ActivityStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("profile: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// bodyPreviewCharsSQL is MaxBodyPreviewChars rendered as a SQL integer. It is a
// compile-time constant, never client input, so interpolating it into the query
// below carries no injection risk.
var bodyPreviewCharsSQL = strconv.Itoa(MaxBodyPreviewChars)

// activityUnion is the merged activity stream: one UNION ALL branch per entity a
// user can author, each already narrowed to that user's rows and shaped into the
// common (kind, id, created_at, payload) columns.
//
// The payload is built in SQL with jsonb_build_object, so the context a card
// needs (a story's title, an adaptation's story title, a comment's preview, a
// bridge's source) travels with the row rather than being fetched per activity:
// the whole page is one query, never N+1 (KNOT-ADR-042).
//
// The version branch excludes root versions (parent_version_id IS NOT NULL): a
// story's root version is the story itself, so counting it again as a "version"
// would list the same act twice. Only adaptations appear as versions.
//
// The comment branch excludes a comment that is the target of a bridge: bridging
// writes both a bridge row and the target comment, and the target is the bridge's
// artifact, so the bridge branch is the one that represents the act. Without this,
// one bridge would appear as two activities.
//
// Comments and bridges join their version (and, for a bridge, its source comment)
// to resolve the story/version ids a client needs to open the right thread.
var activityUnion = `
	SELECT
		'story' AS kind,
		s.id AS id,
		s.created_at AS created_at,
		jsonb_build_object(
			'title', rv.title,
			'pillar', s.pillar,
			'language', rv.language
		) AS payload
	FROM stories s
	JOIN story_versions rv ON rv.id = s.root_version_id
	WHERE s.author_id = $1

	UNION ALL

	SELECT
		'version' AS kind,
		v.id AS id,
		v.created_at AS created_at,
		jsonb_build_object(
			'story_id', v.story_id,
			'story_title', rv.title,
			'language', v.language
		) AS payload
	FROM story_versions v
	JOIN stories s ON s.id = v.story_id
	JOIN story_versions rv ON rv.id = s.root_version_id
	WHERE v.author_id = $1
	  AND v.parent_version_id IS NOT NULL

	UNION ALL

	SELECT
		'comment' AS kind,
		c.id AS id,
		c.created_at AS created_at,
		jsonb_build_object(
			'version_id', c.version_id,
			'story_id', v.story_id,
			'body_preview', left(c.body, ` + bodyPreviewCharsSQL + `)
		) AS payload
	FROM comments c
	JOIN story_versions v ON v.id = c.version_id
	WHERE c.author_id = $1
	  AND NOT EXISTS (SELECT 1 FROM bridges b WHERE b.target_comment_id = c.id)

	UNION ALL

	SELECT
		'bridge' AS kind,
		b.id AS id,
		b.created_at AS created_at,
		jsonb_build_object(
			'source_comment_id', b.source_comment_id,
			'version_id', sc.version_id,
			'target_language', b.target_language
		) AS payload
	FROM bridges b
	JOIN comments sc ON sc.id = b.source_comment_id
	WHERE b.author_id = $1
`

// ListForUser returns one page of a user's wall, newest first, plus the cursor
// that resumes after it (nil when this page is the last one).
//
// Pagination is keyset over the merged stream: the ORDER BY key is
// (created_at DESC, id DESC), so the next page is everything strictly "before"
// the last row of this page in that order. The id tiebreaker is what makes the
// page correct when two activities share a timestamp — which is common here,
// because a story and its root version are written by one statement.
//
// One extra row is fetched to decide whether more remain; that row is not
// returned, and the cursor is derived from the last row that is.
func (s *PostgresStore) ListForUser(ctx context.Context, userID string, cursor *Cursor, limit int) ([]Activity, *Cursor, error) {
	// A malformed id cannot match a row; return an empty wall rather than letting
	// the id reach a SQL cast.
	if !isUUID(userID) {
		return []Activity{}, nil, nil
	}

	query := `SELECT kind, id, created_at, payload FROM (` + activityUnion + `) AS activities`
	args := []any{userID}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		query += ` WHERE (created_at, id) < ($2::timestamptz, $3::uuid)`
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("profile: list activities: %w", err)
	}
	defer rows.Close()

	page := make([]Activity, 0, limit)
	for rows.Next() {
		var (
			activity    Activity
			kind        string
			payloadJSON []byte
		)

		if err := rows.Scan(&kind, &activity.ID, &activity.CreatedAt, &payloadJSON); err != nil {
			return nil, nil, fmt.Errorf("profile: list activities: %w", err)
		}

		activity.Kind = Kind(kind)
		if len(payloadJSON) > 0 {
			if err := json.Unmarshal(payloadJSON, &activity.Payload); err != nil {
				return nil, nil, fmt.Errorf("profile: decode activity payload: %w", err)
			}
		}

		page = append(page, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("profile: list activities: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}
