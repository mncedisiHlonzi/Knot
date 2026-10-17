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

// previewCharsSQL is MaxPreviewChars rendered as a SQL integer, on the same terms.
var previewCharsSQL = strconv.Itoa(MaxPreviewChars)

// activityPreviewSQL builds the payload entries for a card's body excerpt: the
// column's first MaxPreviewChars characters, with an ellipsis appended when the
// body is longer, and a boolean saying whether that happened. Both are enforced
// here rather than in the client so a full body never travels over the wall
// (KNOT-020g-fix4), and so no client has to infer truncation from the text
// (KNOT-020g-fix12). `column` is always a compile-time literal written in the
// union below — never client input — so interpolating it carries no injection
// risk.
func activityPreviewSQL(column string) string {
	return `'preview', CASE
				WHEN length(` + column + `) > ` + previewCharsSQL + ` THEN left(` + column + `, ` + previewCharsSQL + `) || '…'
				ELSE ` + column + `
			END,
			'preview_truncated', length(` + column + `) > ` + previewCharsSQL
}

// activityUnion is the merged activity stream: one UNION ALL branch per entity a
// user can author, each already narrowed to that user's rows and shaped into the
// common (kind, id, created_at, payload) columns.
//
// The payload is built in SQL with jsonb_build_object, so the context a card
// needs (a story's title, place, and version count; an adaptation's story title,
// place, and version number; a comment's preview, an inquiry's place and answer
// count, an answer's question) travels with the row rather than being fetched per
// activity: the whole page is one query, never N+1 (KNOT-ADR-042).
//
// The story branch counts its versions with a correlated COUNT over
// story_versions(story_id) — an index-only lookup for the handful of rows on a
// page. The version branch's ordinal (see `ranked` below) is a ROW_NUMBER over the
// author's own stories' versions, so the window is sized by the author's
// adaptations rather than by the whole table. The inquiry branch counts its
// answers the same way, over inquiry_answers(inquiry_id).
//
// The version branch excludes root versions (parent_version_id IS NOT NULL): a
// story's root version is the story itself, so counting it again as a "version"
// would list the same act twice. Only adaptations appear as versions.
//
// The comment branch joins its version to resolve the story and version ids a
// client needs to open the right thread.
//
// The inquiry and inquiry_answer branches are separate acts by different people:
// an answer always belongs to someone else's inquiry, so the two can never
// describe the same row. An inquiry_answer joins its inquiry to name the question
// it answers.
var activityUnion = `
	SELECT
		'story' AS kind,
		s.id AS id,
		s.created_at AS created_at,
		jsonb_build_object(
			'title', rv.title,
			'pillar', s.pillar,
			'language', rv.language,
			'place', s.approximate_location,
			'version_count', (
				SELECT COUNT(*)
				FROM story_versions sv
				WHERE sv.story_id = s.id
			),
			` + activityPreviewSQL("rv.body") + `
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
			'language', v.language,
			'place', s.approximate_location,
			'version_number', ranked.version_number,
			` + activityPreviewSQL("v.body") + `
		) AS payload
	FROM story_versions v
	JOIN stories s ON s.id = v.story_id
	JOIN story_versions rv ON rv.id = s.root_version_id
	-- The ranked derived table below numbers every version of the stories this
	-- author adapted, counting the root as 1, so an adaptation's version_number is
	-- its true "vN". The window is partitioned per story and ordered oldest-first,
	-- with the id as a tiebreaker (a story and its root version share a timestamp),
	-- so it is deterministic. It is joined on id rather than filtered inline
	-- because a window function is evaluated after WHERE: filtering by author
	-- first would number only that author's rows and lose the root's position.
	JOIN (
		SELECT
			sv.id AS id,
			ROW_NUMBER() OVER (
				PARTITION BY sv.story_id
				ORDER BY sv.created_at, sv.id
			) AS version_number
		FROM story_versions sv
		WHERE sv.story_id IN (
			SELECT DISTINCT v2.story_id
			FROM story_versions v2
			WHERE v2.author_id = $1
			  AND v2.parent_version_id IS NOT NULL
		)
	) ranked ON ranked.id = v.id
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

	UNION ALL

	SELECT
		'inquiry' AS kind,
		i.id AS id,
		i.created_at AS created_at,
		jsonb_build_object(
			'title', i.title,
			'place', i.place,
			'answer_count', (
				SELECT COUNT(*)
				FROM inquiry_answers ans
				WHERE ans.inquiry_id = i.id
			),
			` + activityPreviewSQL("i.body") + `
		) AS payload
	FROM inquiries i
	WHERE i.author_id = $1

	UNION ALL

	SELECT
		'inquiry_answer' AS kind,
		a.id AS id,
		a.created_at AS created_at,
		jsonb_build_object(
			'inquiry_id', a.inquiry_id,
			'inquiry_title', i.title,
			'body_preview', left(a.body, ` + bodyPreviewCharsSQL + `)
		) AS payload
	FROM inquiry_answers a
	JOIN inquiries i ON i.id = a.inquiry_id
	WHERE a.author_id = $1
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
