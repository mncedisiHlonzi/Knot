package storystats

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the pgx-backed implementation of Store.
//
// It holds an injected pool and no other state, so it is safe for concurrent
// use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the read contract.
var _ Store = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("storystats: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CountsForStories returns the story counts for the given ids in four grouped
// queries — one per count type — rather than one query per story.
//
// A story with no rows in a table is simply absent from the map; a caller reads
// a missing entry as the zero Counts, which is exactly right (no versions is
// zero versions).
func (s *PostgresStore) CountsForStories(ctx context.Context, storyIDs []string) (map[string]Counts, error) {
	counts := make(map[string]Counts, len(storyIDs))
	if len(storyIDs) == 0 {
		return counts, nil
	}

	// The same id list binds every count query, so the placeholders are built
	// once. The comparison relies on PostgreSQL inferring the parameter type
	// (uuid) from the column, exactly as FirstMedia does in the media store.
	placeholders, args := placeholdersFor(storyIDs)

	// Versions: every row of the story's tree, the root included.
	if err := s.groupedCount(ctx, placeholders, args,
		`SELECT story_id, COUNT(*) FROM story_versions WHERE story_id IN (`+placeholders+`) GROUP BY story_id`,
		counts, func(c *Counts, n int) { c.Versions = n }); err != nil {
		return nil, err
	}

	// Languages: the distinct language tags across those versions.
	if err := s.groupedCount(ctx, placeholders, args,
		`SELECT story_id, COUNT(DISTINCT language) FROM story_versions WHERE story_id IN (`+placeholders+`) GROUP BY story_id`,
		counts, func(c *Counts, n int) { c.Languages = n }); err != nil {
		return nil, err
	}

	// Comments: every comment on any version of the story, replies included. A
	// comment names the version it belongs to, so the story is reached through
	// story_versions.
	if err := s.groupedCount(ctx, placeholders, args,
		`SELECT sv.story_id, COUNT(*) FROM comments c JOIN story_versions sv ON sv.id = c.version_id WHERE sv.story_id IN (`+placeholders+`) GROUP BY sv.story_id`,
		counts, func(c *Counts, n int) { c.Comments = n }); err != nil {
		return nil, err
	}

	// Media: every attached item.
	if err := s.groupedCount(ctx, placeholders, args,
		`SELECT story_id, COUNT(*) FROM story_media WHERE story_id IN (`+placeholders+`) GROUP BY story_id`,
		counts, func(c *Counts, n int) { c.Media = n }); err != nil {
		return nil, err
	}

	return counts, nil
}

// groupedCount runs one `SELECT story_id, count … GROUP BY story_id` query and
// folds each row into out via assign, so the four count queries in
// CountsForStories share one scanning loop.
//
// It tolerates a nil out map by allocating one, so a caller need not guard.
func (s *PostgresStore) groupedCount(ctx context.Context, placeholders string, args []any, query string, out map[string]Counts, assign func(*Counts, int)) error {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("storystats: count: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			storyID string
			count   int
		)
		if err := rows.Scan(&storyID, &count); err != nil {
			return fmt.Errorf("storystats: count: %w", err)
		}
		entry := out[storyID]
		assign(&entry, count)
		out[storyID] = entry
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storystats: count: %w", err)
	}

	return nil
}

// placeholdersFor returns the "($1, $2, …)" fragment for the ids and the matching
// argument slice, so a batched query binds the whole page in one statement.
func placeholdersFor(ids []string) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	return strings.Join(placeholders, ", "), args
}
