package storystats

// Package storystats is a small read model of the aggregate counts a story
// carries (KNOT-020c-fix).
//
// A feed entry shows how many versions a story has, in how many languages, how
// many comments it has drawn, and how many media items it holds. Those numbers
// live in three different domains — story_versions, comments, and story_media —
// so they do not belong in any one of their stores. This package owns the
// batched, read-only queries that produce them, keeping the counting SQL in one
// place rather than fanning it across the entity stores.
//
// Nothing here writes: every method is a read. Ids are plain strings holding
// canonical UUID text, the convention the rest of the backend uses (KNOT-ADR-010).

import "context"

// Counts is one story's aggregate counts.
//
// Zero is a real answer ("no comments yet"), so the fields are plain ints rather
// than pointers.
type Counts struct {
	// Versions is the number of versions in the story's Language Tree, including
	// the root version.
	Versions int
	// Languages is the number of distinct languages across those versions.
	Languages int
	// Comments is the number of comments on every version of the story, replies
	// included (KNOT-020c-fix).
	Comments int
	// Media is the number of media items attached to the story.
	Media int
}

// Store is the read contract for story counts.
type Store interface {
	// CountsForStories returns the counts for each of the given story ids, keyed
	// by story id. A story with nothing to count is absent from the map, which a
	// caller reads as the zero Counts. It is one grouped query per count type,
	// never a query per story.
	CountsForStories(ctx context.Context, storyIDs []string) (map[string]Counts, error)
}
