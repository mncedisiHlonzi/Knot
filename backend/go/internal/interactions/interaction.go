// Package interactions records and reads user engagement.
//
// It has two halves. The write half (`log.go`) appends one weighted row per
// significant action — view, read_full, reaction, comment, adapt — to
// `user_interactions`, the data foundation of the relevance engine (KNOT-026).
// The read half below is a read model of the latest social-proof interactions
// on a page of feed items.
//
// A feed card heads an entry with the newest things people have done to it: a
// reaction ("rings true", "knows it differently", …) or a comment. The two live in
// different domains — reactions in `reactions`, comments in `comments` — so
// assembling them is a read shape of its own rather than a query in either store,
// exactly as `storystats` is for aggregate counts (KNOT-020c-fix, KNOT-028).
//
// The model deliberately does NOT resolve actor identities. The HTTP layer already
// batches one author read per page, and the interaction actors must ride in that
// same read, or naming them would become a query per actor. The store therefore
// returns actor ids and the handler projects names and avatars.
package interactions

import (
	"context"
	"time"

	"github.com/knot/backend/internal/reactions"
)

// Kind is the sort of interaction a feed card shows.
type Kind string

const (
	// KindReaction is one reader's perspective signal on the entry.
	KindReaction Kind = "reaction"
	// KindComment is one reader's comment on the entry's conversation.
	KindComment Kind = "comment"
)

// Target names the two sources an entry's social proof reads from.
//
// A feed entry's identity keys differently by kind: a story reacts as a story and
// its conversation is the root version's, while an adaptation reacts as a version
// and its conversation is its own (KNOT-021, KNOT-022). Target carries both so the
// store can read a mixed page in one pass.
type Target struct {
	// Key is the feed item id the result is keyed by: the story id for a story
	// entry, the version id for an adaptation.
	Key string
	// ReactionEntityType is "story" for a story entry and "version" for an
	// adaptation.
	ReactionEntityType reactions.EntityType
	// ReactionEntityID is the entity the reactions are attached to: the story id,
	// or the version id.
	ReactionEntityID string
	// CommentVersionID is the version whose comments count as this entry's
	// conversation: the story's root version, or the adaptation itself.
	CommentVersionID string
}

// Interaction is one actor's latest action on a target.
type Interaction struct {
	// Kind says whether this is a reaction or a comment.
	Kind Kind
	// ActorID is the user who acted. The HTTP layer resolves their name and
	// avatar from the page's one batched author read.
	ActorID string
	// Detail is the reaction type (for example "rings_true") for a reaction, and
	// empty for a comment.
	Detail string
	// CreatedAt is when the action happened: the merge key across the two kinds.
	CreatedAt time.Time
	// ID is the row id, used only as a deterministic tie-break between two
	// interactions created in the same transaction.
	ID string
}

// Store is the read contract: at most the newest reaction and the newest comment
// for each target, merged newest-first.
//
// A target with no interactions is absent from the map, which a caller reads as
// "nothing to show". The read is batched — one query for every target's reactions
// and one for every target's comments — so enriching a page never fans out into a
// query per row (KNOT-028).
type Store interface {
	Latest(ctx context.Context, targets []Target) (map[string][]Interaction, error)
}
