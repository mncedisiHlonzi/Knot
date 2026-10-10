package httpapi

import (
	"context"
	"log/slog"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/rooted"
	"github.com/knot/backend/internal/storymedia"
)

// StoryMediaLookup is the story-media enrichment dependency: the full list for
// one story, and a batched read of each story's first item for the feed.
//
// It is deliberately narrower than the whole storymedia service. The stories
// handler only decorates a response with a story's media, so it depends on the
// two reads that do that rather than on upload, download, and delete.
type StoryMediaLookup interface {
	ListMedia(ctx context.Context, storyID string) ([]storymedia.StoryMedia, error)
	FirstMedia(ctx context.Context, storyIDs []string) (map[string]storymedia.StoryMedia, error)
}

// storyMediaForDetail returns a story's full media list, in display order.
//
// Enrichment is supplementary, so a failure is logged and swallowed rather than
// failing the whole response: a problem reading media must not make a story
// unreadable. The result is always non-nil so the response carries [] rather than
// null.
func storyMediaForDetail(ctx context.Context, lookup StoryMediaLookup, logger *slog.Logger, storyID string) []storyMediaResponse {
	media, err := lookup.ListMedia(ctx, storyID)
	if err != nil {
		logger.WarnContext(
			ctx,
			"story media enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("story_id", storyID),
			slog.String("error", err.Error()),
		)
		return []storyMediaResponse{}
	}

	out := make([]storyMediaResponse, 0, len(media))
	for _, item := range media {
		out = append(out, newStoryMediaResponse(item, storyID))
	}

	return out
}

// storyMediaPreviews returns a single-item preview for each of the given stories
// that has any media, keyed by story id. It is one batched read, so enriching a
// feed page does not fan out into a query per story.
func storyMediaPreviews(ctx context.Context, lookup StoryMediaLookup, logger *slog.Logger, storyIDs []string) map[string][]storyMediaResponse {
	if len(storyIDs) == 0 {
		return nil
	}

	first, err := lookup.FirstMedia(ctx, storyIDs)
	if err != nil {
		logger.WarnContext(
			ctx,
			"story media enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	previews := make(map[string][]storyMediaResponse, len(first))
	for storyID, item := range first {
		previews[storyID] = []storyMediaResponse{newStoryMediaResponse(item, storyID)}
	}

	return previews
}

// RootedLookup is the enrichment dependency: one batched read of the primary
// public Rooted signal for a set of authors.
//
// It is deliberately narrower than the whole Rooted service. The stories,
// versions, and conversations handlers only need to decorate a response with an
// author's inline Rooted summary, so they depend on the single method that does
// that rather than on signal writing and reading.
type RootedLookup interface {
	BatchGetPrimaryPublicSignals(ctx context.Context, userIDs []string) (map[string]*rooted.Signal, error)
}

// rootedSummary is the inline Rooted projection attached to content responses.
//
// It carries only what an inline badge needs. The signal's id, its owner, and its
// timestamps are deliberately absent: the enrichment is a display hint, not a
// lookup of the full signal, so nothing that identifies the owner beyond their
// already-public author id leaves the building here (KNOT-ADR-017).
type rootedSummary struct {
	Place          string                `json:"place"`
	DurationBucket rooted.DurationBucket `json:"duration_bucket"`
}

// authorRootedSummaries loads the primary public Rooted signal for every distinct
// author id in one call and returns a map from author id to the inline summary.
//
// Every response that carries authored entities goes through here once, so the
// enrichment is a single batch query per response rather than one query per row.
//
// Enrichment is supplementary, so a failure is logged and swallowed rather than
// failing the whole response: a problem reading Rooted must not make stories,
// versions, comments, or bridges unreadable. A user with no public signal simply
// has no entry, and the response carries null for them.
func authorRootedSummaries(ctx context.Context, lookup RootedLookup, logger *slog.Logger, authorIDs []string) map[string]*rootedSummary {
	unique := uniqueAuthorIDs(authorIDs)
	if len(unique) == 0 {
		return nil
	}

	signals, err := lookup.BatchGetPrimaryPublicSignals(ctx, unique)
	if err != nil {
		logger.WarnContext(
			ctx,
			"rooted enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	if len(signals) == 0 {
		return nil
	}

	summaries := make(map[string]*rootedSummary, len(signals))
	for authorID, signal := range signals {
		if signal == nil {
			continue
		}
		summaries[authorID] = &rootedSummary{
			Place:          signal.Place,
			DurationBucket: signal.DurationBucket,
		}
	}

	return summaries
}

// uniqueAuthorIDs returns the distinct, non-empty ids in the order they first
// appear, so the batch query carries each author once.
func uniqueAuthorIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}

	return out
}

// AuthorLookup is the attribution dependency: one batched read of the accounts
// behind a set of author ids.
//
// It is deliberately narrower than the whole identity service. A content handler
// only needs to name and picture an author, not to register, log in, or change an
// account, so it depends on the single method that does that. It is the same
// shape the notifications handler already consumes, so the two share one contract
// (KNOT-ADR-041).
type AuthorLookup interface {
	UsersByIDs(ctx context.Context, ids []string) (map[string]*identity.User, error)
}

// authorAttribution is the resolved public attribution of one author: the display
// name to show, and the backend path of their avatar ("" when they have none).
type authorAttribution struct {
	// DisplayName is the author's name, or "" when their account could not be
	// resolved (for example, a deleted user).
	DisplayName string
	// AvatarURL is the backend path to fetch the avatar from (see avatarPathFor),
	// or "" when the author has no avatar. The wire projection turns "" into null
	// so the client renders initials.
	AvatarURL string
}

// authorAttributions loads the display name and avatar of every distinct author id
// in one batched call, keyed by author id.
//
// It is the parallel of authorRootedSummaries: every response that names an author
// goes through here once, so attribution costs one query per response rather than
// one per row (KNOT-ADR-041).
//
// Enrichment is supplementary, so a failure is logged and swallowed rather than
// failing the whole response: content must stay readable when identity is
// unavailable. An author with no resolvable account has no entry, and the response
// carries an empty display name and a null avatar for them.
func authorAttributions(ctx context.Context, lookup AuthorLookup, logger *slog.Logger, authorIDs []string) map[string]authorAttribution {
	unique := uniqueAuthorIDs(authorIDs)
	if len(unique) == 0 {
		return nil
	}

	users, err := lookup.UsersByIDs(ctx, unique)
	if err != nil {
		logger.WarnContext(
			ctx,
			"author enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	if len(users) == 0 {
		return nil
	}

	attributions := make(map[string]authorAttribution, len(users))
	for authorID, user := range users {
		if user == nil {
			continue
		}
		attributions[authorID] = authorAttribution{
			DisplayName: user.DisplayName,
			AvatarURL:   avatarPathFor(user),
		}
	}

	return attributions
}

// authorFields resolves a single author's display name and wire avatar (null when
// they have none) in one batched read. It is the single-entity counterpart of
// authorAttributions, for the routes that project exactly one author.
func authorFields(ctx context.Context, authors AuthorLookup, logger *slog.Logger, authorID string) (string, *string) {
	attribution := authorAttributions(ctx, authors, logger, []string{authorID})[authorID]
	return attribution.DisplayName, optionalString(attribution.AvatarURL)
}

// ReactionsLookup is the reaction-enrichment dependency: the counts for a page of
// entities of one type, and the signals the current user holds on that page.
//
// It is deliberately narrower than the whole reactions service. A content handler
// only decorates a response with reaction counts and the reader's own signals, so
// it depends on the two batched reads that do that rather than on toggling or
// listing individual reactions.
type ReactionsLookup interface {
	BatchSummaries(ctx context.Context, entityType reactions.EntityType, entityIDs []string) (map[string]reactions.Summary, error)
	BatchMyReactions(ctx context.Context, userID string, entityType reactions.EntityType, entityIDs []string) (map[string][]reactions.ReactionType, error)
}

// reactionCountsResponse is the wire projection of a reaction summary: one count
// per perspective signal, always all four, never null. Zero is a real answer (no
// one left that signal), so the fields are ints rather than pointers.
type reactionCountsResponse struct {
	RingsTrue         int `json:"rings_true"`
	KnowItDifferently int `json:"know_it_differently"`
	AddsSomethingNew  int `json:"adds_something_new"`
	NeedsASource      int `json:"needs_a_source"`
}

// newReactionCounts projects a domain summary onto the wire format.
func newReactionCounts(summary reactions.Summary) reactionCountsResponse {
	return reactionCountsResponse{
		RingsTrue:         summary.RingsTrue,
		KnowItDifferently: summary.KnowItDifferently,
		AddsSomethingNew:  summary.AddsSomethingNew,
		NeedsASource:      summary.NeedsASource,
	}
}

// Total returns the sum of the four counts.
func (c reactionCountsResponse) Total() int {
	return c.RingsTrue + c.KnowItDifferently + c.AddsSomethingNew + c.NeedsASource
}

// reactionTypeStrings projects a list of reaction types onto the wire format. It
// always returns a non-nil slice so an entity the reader has not reacted to
// serialises as [] rather than null.
func reactionTypeStrings(types []reactions.ReactionType) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

// reactionEnrichment is the batched reaction decoration for one page of entities,
// keyed by entity id.
type reactionEnrichment struct {
	// Counts is the summary per entity id. A missed id is the zero summary.
	Counts map[string]reactionCountsResponse
	// Mine is the reader's own signals per entity id. A missed id is empty.
	Mine map[string][]string
}

// loadReactions decorates a page of entities with reaction counts and, when a
// reader is signed in, the signals that reader holds.
//
// It is two batched reads (one for the counts, one for the reader's own signals)
// regardless of page size, so enriching a feed or a tree never fans out into a
// query per row.
//
// Enrichment is supplementary, so a lookup failure is logged and swallowed rather
// than failing the whole response: content must stay readable when reactions are
// unavailable, and the reader simply sees zero counts and no highlights.
func loadReactions(ctx context.Context, lookup ReactionsLookup, logger *slog.Logger, entityType reactions.EntityType, userID string, entityIDs []string) reactionEnrichment {
	out := reactionEnrichment{
		Counts: make(map[string]reactionCountsResponse, len(entityIDs)),
		Mine:   make(map[string][]string, len(entityIDs)),
	}
	if len(entityIDs) == 0 {
		return out
	}

	summaries, err := lookup.BatchSummaries(ctx, entityType, entityIDs)
	if err != nil {
		logger.WarnContext(
			ctx,
			"reaction enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("entity_type", string(entityType)),
			slog.String("error", err.Error()),
		)
	} else {
		for id, summary := range summaries {
			out.Counts[id] = newReactionCounts(summary)
		}
	}

	if userID == "" {
		return out
	}

	mine, err := lookup.BatchMyReactions(ctx, userID, entityType, entityIDs)
	if err != nil {
		logger.WarnContext(
			ctx,
			"my-reactions enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("entity_type", string(entityType)),
			slog.String("error", err.Error()),
		)
		return out
	}
	for id, types := range mine {
		out.Mine[id] = reactionTypeStrings(types)
	}

	return out
}

// reactionsForEntity decorates a single entity. It is the one-entity counterpart
// of loadReactions, for the detail routes.
func reactionsForEntity(ctx context.Context, lookup ReactionsLookup, logger *slog.Logger, entityType reactions.EntityType, userID, entityID string) (reactionCountsResponse, []string) {
	enrichment := loadReactions(ctx, lookup, logger, entityType, userID, []string{entityID})
	return enrichment.Counts[entityID], orEmptyStrings(enrichment.Mine[entityID])
}

// orEmptyStrings returns a non-nil slice so a wire field serialises as [] rather
// than null when there is nothing to report.
func orEmptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
