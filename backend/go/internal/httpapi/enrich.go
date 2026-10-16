package httpapi

import (
	"context"
	"log/slog"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/rooted"
	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/storystats"
	"github.com/knot/backend/internal/versions"
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

// chainWithNames fills each link of a version chain with its author's display
// name, so the attribution sentence can name people.
//
// The versions domain deliberately leaves the name empty: resolving a chain must
// not become a query per link, so the domain resolves structure and the HTTP
// layer resolves identity, from a map it either batches here or has already
// loaded (the tree names its whole page once).
func chainWithNames(chain versions.Chain, names map[string]authorAttribution) versions.Chain {
	if len(chain) == 0 {
		return chain
	}

	named := make(versions.Chain, 0, len(chain))
	for _, link := range chain {
		link.AuthorDisplayName = names[link.AuthorID].DisplayName
		named = append(named, link)
	}

	return named
}

// attributionText renders one version's chain as the sentence a reader sees,
// naming every link's author from one batched read.
//
// It is the single-entity path: the version and adapt responses resolve exactly
// one chain, so the author read is scoped to the links that chain holds. The tree
// endpoint uses chainWithNames directly instead, because it has already named
// every author on the page (KNOT-021).
func attributionText(ctx context.Context, authors AuthorLookup, logger *slog.Logger, chain versions.Chain) string {
	if len(chain) == 0 {
		return ""
	}

	ids := make([]string, 0, len(chain))
	for _, link := range chain {
		ids = append(ids, link.AuthorID)
	}

	return versions.FormatAttribution(chainWithNames(chain, authorAttributions(ctx, authors, logger, ids)))
}

// VersionChainLookup is the attribution-enrichment dependency for a page of
// versions: the lineage of each requested version, keyed by version id.
//
// It exists because the feed lists stories and adaptations in one stream, so it
// must attribute a whole page at once rather than walk one chain per entry. It is
// deliberately narrower than the whole versions service — the feed only needs the
// chains, never to write a version — and the implementation is expected to resolve
// the whole batch from one read (KNOT-022, KNOT-ADR-069).
type VersionChainLookup interface {
	ChainsFor(ctx context.Context, versionIDs []string) (map[string]versions.Chain, error)
}

// versionChains loads the lineage of a page of versions in one batched call,
// keyed by version id.
//
// It is the sibling of authorAttributions and loadReactions: one read per
// response, never one per row.
//
// Attribution is supplementary, so a failure is logged and swallowed rather than
// failing the whole response: an adaptation must stay readable when its lineage
// cannot be resolved, and the caller simply shows no attribution sentence. A
// version whose chain could not be built is absent from the map, which the caller
// reads the same way.
func versionChains(ctx context.Context, lookup VersionChainLookup, logger *slog.Logger, versionIDs []string) map[string]versions.Chain {
	if len(versionIDs) == 0 {
		return nil
	}

	chains, err := lookup.ChainsFor(ctx, versionIDs)
	if err != nil {
		logger.WarnContext(
			ctx,
			"version chain enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	return chains
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

// StoryStatsLookup is the aggregate-counts enrichment dependency: one batched
// read of the version, language, comment, and media counts for a set of stories.
//
// It is deliberately narrower than the whole storystats store. A content handler
// only decorates a story response with its counts, so it depends on the single
// batched read that does that (KNOT-020c-fix).
type StoryStatsLookup interface {
	CountsForStories(ctx context.Context, storyIDs []string) (map[string]storystats.Counts, error)
}

// storyStats loads the aggregate counts for a page of stories in one batched
// call, keyed by story id.
//
// It is the sibling of authorAttributions and loadReactions: one read per
// response, never one per row, so enriching a feed page does not fan out into a
// query per story.
//
// Enrichment is supplementary, so a failure is logged and swallowed rather than
// failing the whole response: a story must stay readable when its counts cannot
// be read, and the caller simply shows zeros. A story with no counts in any table
// has no entry, which the caller reads as the zero Counts.
func storyStats(ctx context.Context, lookup StoryStatsLookup, logger *slog.Logger, storyIDs []string) map[string]storystats.Counts {
	if len(storyIDs) == 0 {
		return nil
	}

	counts, err := lookup.CountsForStories(ctx, storyIDs)
	if err != nil {
		logger.WarnContext(
			ctx,
			"story stats enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	return counts
}

// storyPageLookups bundles the batched reads that decorate a page of story
// responses: the author attribution and inline Rooted summary of each story's
// author, the story's media preview, its aggregate counts, and its reactions.
//
// It exists so that every surface which lists stories — the feed (GET /stories)
// and a place page (GET /discovery/places/{place}) — decorates them through one
// code path. A story then carries the same fields with the same values wherever
// it is listed, and the two surfaces cannot drift apart (KNOT-020e-fix2).
type storyPageLookups struct {
	authors   AuthorLookup
	rooted    RootedLookup
	media     StoryMediaLookup
	reactions ReactionsLookup
	stats     StoryStatsLookup
	logger    *slog.Logger
}

// enrich decorates a page of story responses in place.
//
// It is one batched read per lookup regardless of page size, so listing a page
// of stories is a fixed number of queries rather than one per row. Every
// enrichment is supplementary: a lookup failure is logged and swallowed by the
// helper it belongs to, so a story stays readable when a dependency is down.
func (l storyPageLookups) enrich(ctx context.Context, items []storyResponse) {
	if len(items) == 0 {
		return
	}

	ids := make([]string, 0, len(items))
	authorIDs := make([]string, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
		authorIDs = append(authorIDs, items[i].AuthorID)
	}

	// One batched read names and pictures every author on the page, and one more
	// attaches that author's inline Rooted summary.
	attributions := authorAttributions(ctx, l.authors, l.logger, authorIDs)
	rootedSummaries := authorRootedSummaries(ctx, l.rooted, l.logger, authorIDs)

	// One batched read attaches a single-item media preview to each story, and one
	// more the aggregate counts.
	previews := storyMediaPreviews(ctx, l.media, l.logger, ids)
	counts := storyStats(ctx, l.stats, l.logger, ids)

	// One batched pair of reads decorates the page with the reaction counts and,
	// when a reader is signed in, the signals that reader holds.
	readerID, _ := UserIDFromContext(ctx)
	reactionData := loadReactions(ctx, l.reactions, l.logger, reactions.EntityStory, readerID, ids)

	for i := range items {
		applyStoryCardFields(&items[i], attributions, rootedSummaries, previews, counts)
		items[i].Reactions = reactionData.Counts[items[i].ID]
		items[i].MyReactions = orEmptyStrings(reactionData.Mine[items[i].ID])
	}
}

// applyStoryCardFields fills one story-shaped entry's enriched fields from the
// batched maps a caller has already loaded: the author's name and picture, their
// inline Rooted summary, the media preview, and the aggregate counts with the
// first-media fields derived from that preview.
//
// It is shared by the two surfaces that decorate a story entry -- the place page
// and the feed -- so a story carries identical fields with identical values
// wherever it is listed, even where the two surfaces batch their reads differently
// (KNOT-020e-fix2, KNOT-022). Reactions are set by the caller, because the feed and
// the place page read them for different entity sets.
func applyStoryCardFields(
	item *storyResponse,
	attributions map[string]authorAttribution,
	rootedSummaries map[string]*rootedSummary,
	previews map[string][]storyMediaResponse,
	counts map[string]storystats.Counts,
) {
	attribution := attributions[item.AuthorID]
	item.AuthorDisplayName = attribution.DisplayName
	item.AuthorAvatarURL = optionalString(attribution.AvatarURL)
	item.AuthorRooted = rootedSummaries[item.AuthorID]

	if preview, ok := previews[item.ID]; ok {
		item.Media = preview
	}

	entry := counts[item.ID]
	item.VersionCount = entry.Versions
	item.LanguageCount = entry.Languages
	item.CommentCount = entry.Comments
	item.MediaCount = entry.Media

	first := firstMediaFields(item.Media)
	item.FirstMediaType = first.mediaType
	item.FirstMediaURL = first.url
	item.FirstMediaWidth = first.width
	item.FirstMediaHeight = first.height
}

// feedPageLookups bundles the batched reads that decorate one page of the MERGED
// feed: both entry kinds at once.
//
// It is separate from storyPageLookups because the feed's two kinds key
// differently, and mixing the keys would silently attach the wrong rows:
//
//   - an entry's author is its own author: an adaptation's is the ADAPTER, not the
//     story's original author, which is why the lineage's links are named in the
//     same read;
//   - media and counts key by the STORY, which for an adaptation is its parent --
//     a version row carries no place or media of its own (KNOT-ADR-030);
//   - reactions key by the VERSION for an adaptation and by the story for a story,
//     because a reader reacts to the adaptation they are reading (KNOT-021).
//
// The story entries are still filled by applyStoryCardFields, the same function the
// place page uses, so a story carries identical fields on both surfaces. What
// differs is only how many reads the page's two kinds share: the identity, Rooted,
// media, and count lookups span both kinds in one call each, because their keys can
// be unioned without ambiguity. Reactions cannot -- an adaptation and a story are
// different reaction entities -- so that lookup runs once per kind.
type feedPageLookups struct {
	authors   AuthorLookup
	rooted    RootedLookup
	media     StoryMediaLookup
	reactions ReactionsLookup
	stats     StoryStatsLookup
	chains    VersionChainLookup
	logger    *slog.Logger
}

// enrich decorates one page of the merged feed in place: the story entries and the
// adaptation entries together, so a mixed page costs one read per lookup plus the
// two reaction reads the two entity types need.
//
// Every enrichment is supplementary: a lookup failure is logged and swallowed by
// the helper it belongs to, so an entry stays readable when a dependency is down.
func (l feedPageLookups) enrich(ctx context.Context, stories []storyResponse, versions []feedVersionItem) {
	if len(stories) == 0 && len(versions) == 0 {
		return
	}

	storyIDs := make([]string, 0, len(stories))
	authorIDs := make([]string, 0, len(stories)+len(versions))
	mediaIDs := make([]string, 0, len(stories)+len(versions))
	versionIDs := make([]string, 0, len(versions))
	parentStoryIDs := make([]string, 0, len(versions))

	for i := range stories {
		storyIDs = append(storyIDs, stories[i].ID)
		authorIDs = append(authorIDs, stories[i].AuthorID)
		mediaIDs = append(mediaIDs, stories[i].ID)
	}
	for i := range versions {
		versionIDs = append(versionIDs, versions[i].ID)
		parentStoryIDs = append(parentStoryIDs, versions[i].StoryID)
		authorIDs = append(authorIDs, versions[i].AuthorID)
		mediaIDs = append(mediaIDs, versions[i].StoryID)
	}

	// The lineages are resolved FIRST, because each one names the people in it --
	// the original authors an adaptation descends from -- and those names must ride
	// in the same batched author read as the entries' own authors rather than
	// becoming a read per link (KNOT-021, KNOT-022).
	chains := versionChains(ctx, l.chains, l.logger, versionIDs)
	for _, chain := range chains {
		for _, link := range chain {
			authorIDs = append(authorIDs, link.AuthorID)
		}
	}

	// One read names and pictures every person the page mentions, and one more
	// attaches the inline Rooted summary of every author who is ON the page. The
	// originals named inside an attribution sentence are deliberately not looked up:
	// only the person on the card has a Rooted signal shown next to their name
	// (KNOT-ADR-017).
	attributions := authorAttributions(ctx, l.authors, l.logger, authorIDs)
	rootedSummaries := authorRootedSummaries(ctx, l.rooted, l.logger, append(append([]string{}, storyAuthorIDs(stories)...), versionAuthorIDs(versions)...))

	// One read attaches the media preview of every story the page mentions -- the
	// stories themselves and the stories the adaptations adapt -- and one more their
	// aggregate counts. Two entries that resolve to the same story resolve to the
	// same row, which is why both lookups key by story id.
	previews := storyMediaPreviews(ctx, l.media, l.logger, mediaIDs)
	counts := storyStats(ctx, l.stats, l.logger, mediaIDs)

	for i := range stories {
		applyStoryCardFields(&stories[i], attributions, rootedSummaries, previews, counts)
	}
	for i := range versions {
		applyVersionCardFields(&versions[i], chains, attributions, rootedSummaries, previews, counts)
	}

	// Reactions are the one lookup that cannot span both kinds: a story entry
	// reacts as a story and an adaptation as a version, so the page reads each
	// entity set once. When a page carries one kind only, the other read is skipped
	// rather than issued empty.
	readerID, _ := UserIDFromContext(ctx)

	storyReactions := loadReactions(ctx, l.reactions, l.logger, reactions.EntityStory, readerID, storyIDs)
	for i := range stories {
		stories[i].Reactions = storyReactions.Counts[stories[i].ID]
		stories[i].MyReactions = orEmptyStrings(storyReactions.Mine[stories[i].ID])
	}

	versionReactions := loadReactions(ctx, l.reactions, l.logger, reactions.EntityVersion, readerID, versionIDs)
	for i := range versions {
		versions[i].Reactions = versionReactions.Counts[versions[i].ID]
		versions[i].MyReactions = orEmptyStrings(versionReactions.Mine[versions[i].ID])
	}
}

// applyVersionCardFields fills one adaptation entry's enriched fields from the
// batched maps the caller loaded, and derives its version number and attribution
// sentence from its lineage.
//
// The keys are the ones described on feedPageLookups: the story's media and counts,
// the adapter's identity, and the version's own reactions (set by the caller).
// Attribution is enrichment, so an entry whose lineage could not be resolved is
// still filled -- it simply carries no number and no sentence (KNOT-021).
func applyVersionCardFields(
	item *feedVersionItem,
	chains map[string]versions.Chain,
	attributions map[string]authorAttribution,
	rootedSummaries map[string]*rootedSummary,
	previews map[string][]storyMediaResponse,
	counts map[string]storystats.Counts,
) {
	attribution := attributions[item.AuthorID]
	item.AuthorDisplayName = attribution.DisplayName
	item.AuthorAvatarURL = optionalString(attribution.AvatarURL)
	item.AuthorRooted = rootedSummaries[item.AuthorID]

	// The story's media and counts: an adaptation entry shows the story behind it.
	entry := counts[item.StoryID]
	item.VersionCount = entry.Versions
	item.LanguageCount = entry.Languages
	item.CommentCount = entry.Comments
	item.MediaCount = entry.Media

	first := firstMediaFields(previews[item.StoryID])
	item.FirstMediaType = first.mediaType
	item.FirstMediaURL = first.url
	item.FirstMediaWidth = first.width
	item.FirstMediaHeight = first.height

	chain, ok := chains[item.ID]
	if !ok {
		return
	}

	// VersionNumber counts the original as 1, so a chain of depth n -- n
	// adaptations away from the root -- is version n+1.
	item.VersionNumber = chain.Depth() + 1
	item.AttributionLine = versions.FormatAttribution(chainWithNames(chain, attributions))
}

// storyAuthorIDs returns the author of each story entry.
func storyAuthorIDs(stories []storyResponse) []string {
	ids := make([]string, 0, len(stories))
	for i := range stories {
		ids = append(ids, stories[i].AuthorID)
	}

	return ids
}

// versionAuthorIDs returns the adapter of each adaptation entry.
func versionAuthorIDs(versions []feedVersionItem) []string {
	ids := make([]string, 0, len(versions))
	for i := range versions {
		ids = append(ids, versions[i].AuthorID)
	}

	return ids
}
