package feedcache

// Resolving the audience of a story (KNOT-036).
//
// Knot has no follower graph. A reader should see a story when the author is
// Rooted in a place the reader cares about, or when the story is written in a
// language the reader speaks. That pair of rules is the fan-out target, and it is
// a query against rooted_signals and users.preferred_languages rather than a walk
// of a graph.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AudienceQuery is one story's fan-out question.
type AudienceQuery struct {
	// AuthorID is the story's author. Their own primary Rooted signal supplies the
	// place, and they are never a recipient.
	AuthorID string
	// Language is the story's ISO 639-3 code.
	Language string
	// Limit bounds how many ids are returned. The caller asks for one more than
	// its fan-out threshold, so an over-large audience is detected without being
	// materialised.
	Limit int
}

// Audience resolves the readers who should receive a story.
//
// It is an interface so the fan-out rules can be tested without a database: the
// fixture that matters is "these are the ids", not how SQL found them.
type Audience interface {
	// TargetUserIDs returns at most q.Limit user ids, never including the author.
	//
	// A limit below 1 is a caller bug and yields no ids rather than an unbounded
	// query.
	TargetUserIDs(ctx context.Context, q AudienceQuery) ([]string, error)
}

// PostgresAudience is the pgx-backed Audience.
type PostgresAudience struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the Postgres implementation satisfies the contract.
var _ Audience = (*PostgresAudience)(nil)

// NewPostgresAudience returns an audience lookup backed by pool.
func NewPostgresAudience(pool *pgxpool.Pool) (*PostgresAudience, error) {
	if pool == nil {
		return nil, fmt.Errorf("feedcache: audience requires a non-nil pool")
	}
	return &PostgresAudience{pool: pool}, nil
}

// audienceQuery resolves one story's audience in a single statement.
//
// The author's primary Rooted signal is read in a CTE, so the place and country
// the comparison needs cost no second round trip; an author with no signal yields
// no place, and the place clauses then match nothing (a comparison against NULL is
// never true), leaving language as the only rule — which is the honest answer for
// an author who has declared no Rooted place.
//
// Only PRIMARY signals route, matching the Rooted domain's own rule: a hidden or
// secondary signal is not a statement about where someone belongs today. Deleted
// accounts are excluded, because their feed would never be read again.
//
// The ORDER BY is total (created_at, id) but has no pagination meaning: it exists
// so the same audience always arrives in the same order, which makes a fan-out
// reproducible when it is retried.
const audienceQuery = `
	WITH author_signal AS (
		SELECT rs.place, rs.place_country
		FROM rooted_signals rs
		WHERE rs.user_id = $1::uuid
			AND rs.is_primary = true
		LIMIT 1
	)
	SELECT u.id::text
	FROM users u
	WHERE u.id <> $1::uuid
		AND u.deleted_at IS NULL
		AND (
			$2::text = ANY(u.preferred_languages)
			OR EXISTS (
				SELECT 1
				FROM rooted_signals rs
				WHERE rs.user_id = u.id
					AND rs.is_primary = true
					AND (
						rs.place = (SELECT place FROM author_signal)
						OR rs.place_country = (SELECT place_country FROM author_signal)
					)
			)
		)
	ORDER BY u.created_at ASC, u.id ASC
	LIMIT $3`

// TargetUserIDs returns at most q.Limit readers for the story q describes.
func (a *PostgresAudience) TargetUserIDs(ctx context.Context, q AudienceQuery) ([]string, error) {
	if q.Limit < 1 || q.AuthorID == "" {
		return []string{}, nil
	}

	rows, err := a.pool.Query(ctx, audienceQuery, q.AuthorID, q.Language, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("feedcache: audience query: %w", err)
	}
	defer rows.Close()

	userIDs := make([]string, 0, q.Limit)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("feedcache: audience query: %w", err)
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedcache: audience query: %w", err)
	}

	return userIDs, nil
}
