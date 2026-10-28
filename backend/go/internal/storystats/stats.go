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
	// RootComments is the number of comments on the story's root version, replies
	// included: the thread a reader reaches by tapping the story, and the number a
	// story entry in the feed reports (KNOT-036-fix5).
	RootComments int
	// Comments is the number of comments across every version of the story, replies
	// included. It is the sum of CommentCountByVersion and is kept as the story's
	// own activity total; no response field reads it directly -- story and version
	// entries both report a version-scoped count (KNOT-036-fix5).
	Comments int
	// CommentCountByVersion is the number of comments on each version of the
	// story, keyed by version id. A version absent from the map has no comments,
	// which a caller reads as zero.
	//
	// Replies ARE counted, because the thread does: GET /versions/{id}/comments
	// returns a version's top-level comments and their replies in one list, and the
	// conversation header shows that list's length. Counting only top-level comments
	// would make a feed entry report a smaller number than the page it opens
	// (KNOT-036-fix5).
	CommentCountByVersion map[string]int
	// Media is the number of media items attached to the story.
	Media int
}

// CommentCountForVersion returns the number of comments on one version of the
// story, replies included, and zero when the version is not part of its tree.
//
// It reads the per-version map rather than a story-wide total, so the number it
// answers is exactly the thread the reader sees when they open that version
// (KNOT-036-fix5). Reading a nil map is fine, so the zero Counts answers zero.
func (c Counts) CommentCountForVersion(versionID string) int {
	return c.CommentCountByVersion[versionID]
}

// Store is the read contract for story counts.
type Store interface {
	// CountsForStories returns the counts for each of the given story ids, keyed
	// by story id. A story with nothing to count is absent from the map, which a
	// caller reads as the zero Counts. It is one grouped query per count type,
	// never a query per story.
	CountsForStories(ctx context.Context, storyIDs []string) (map[string]Counts, error)
	// CommentCountForVersion returns the number of comments on one version,
	// replies included: the count a version detail reports, so it can never
	// disagree with the feed entry that leads to it (KNOT-036-fix5).
	CommentCountForVersion(ctx context.Context, versionID string) (int, error)
}
