package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/relevance"
	"github.com/knot/backend/internal/stories"
)

// The feed's two orderings. `recent` is the chronological merged feed this
// endpoint has always served; `for_you` runs the relevance engine over the same
// entries (KNOT-027).
const (
	feedTypeRecent = "recent"
	feedTypeForYou = "for_you"
)

// parseFeedType validates the `feed_type` query parameter. An absent value is
// `recent`, which keeps every existing client on the behaviour it already knows.
func parseFeedType(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", feedTypeRecent:
		return feedTypeRecent, nil
	case feedTypeForYou:
		return feedTypeForYou, nil
	default:
		return "", &stories.ValidationError{
			Field:   "feed_type",
			Message: "must be recent or for_you",
		}
	}
}

// ProfileLookup loads the reader's profile for ranking. It is the identity
// service's one-method view.
type ProfileLookup interface {
	UserByID(ctx context.Context, id string) (*identity.User, error)
}

// EngagementLookup answers the reader's weighted engagement per pillar. It is
// the interactions store's one-method view.
type EngagementLookup interface {
	PillarEngagement(ctx context.Context, userID string) (map[string]float64, error)
}

// EntityFeatureLookup loads the entity_features read model in one batch.
type EntityFeatureLookup interface {
	FeaturesFor(ctx context.Context, entityIDs []string) (map[string]stories.EntityFeature, error)
}

// FeedRanking bundles the three dependencies the `for_you` feed needs.
//
// They are optional: the chronological feed works without them, and a handler
// built with the zero value answers 500 for `for_you` rather than silently
// falling back to the unranked order (a silent fallback would make a broken
// ranking indistinguishable from a correct one).
type FeedRanking struct {
	Profiles   ProfileLookup
	Engagement EngagementLookup
	Features   EntityFeatureLookup
}

// enabled reports whether every dependency is wired.
func (r FeedRanking) enabled() bool {
	return r.Profiles != nil && r.Engagement != nil && r.Features != nil
}

// rankedCandidateWindow is how many of the newest entries the ranker considers
// for one ranked page.
//
// The ranked feed is a ranking of a bounded window, not of the whole table: the
// ranker is a pure function of the candidates it is handed, and fetching every
// story on every request would be unbounded work. 200 is four maximum-size
// chronological pages, so the ranked feed can serve several pages before the
// window is exhausted; a larger corpus needs the ranker to move into the store
// (KNOT-027, disclosed).
const rankedCandidateWindow = 200

// rankedCursor is the opaque position a ranked page resumes from: the rank, the
// creation time and the entry id of the last item delivered.
//
// It is a *position in the reader's ranked order*, so it is meaningful only for
// that reader and that scan of the candidate window. It is base64url-encoded
// JSON, matching the existing cursor's opaque-token pattern, and it is validated
// strictly on the way back in (finite rank, RFC 3339 time, canonical UUID). It is
// not signed — the deployment has no cursor-signing key, and a forged cursor can
// only move the reader's own position in their own feed (disclosed).
type rankedCursor struct {
	Rank      float64   `json:"rank"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

// encodeRankedCursor renders a ranked position as an opaque URL-safe token.
func encodeRankedCursor(item relevance.RankedStory) string {
	payload, err := json.Marshal(rankedCursor{
		Rank:      item.Score,
		CreatedAt: item.CreatedAt.UTC(),
		ID:        rankedEntryID(item),
	})
	if err != nil {
		// A struct of float/string/time cannot fail to marshal; the fallback keeps
		// the return type honest without an error path no caller could act on.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// decodeRankedCursor parses a token produced by encodeRankedCursor. Every
// failure is a client-supplied bad request, never a server fault.
func decodeRankedCursor(raw string) (rankedCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return rankedCursor{}, invalidRankedCursor("must be a base64url-encoded token issued by this API")
	}

	var cursor rankedCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return rankedCursor{}, invalidRankedCursor("is not a ranked-feed cursor")
	}

	if math.IsNaN(cursor.Rank) || math.IsInf(cursor.Rank, 0) {
		return rankedCursor{}, invalidRankedCursor("has a malformed rank")
	}
	if cursor.CreatedAt.IsZero() {
		return rankedCursor{}, invalidRankedCursor("is missing its timestamp")
	}
	if !isCanonicalUUID(cursor.ID) {
		return rankedCursor{}, invalidRankedCursor("has a malformed id")
	}

	return rankedCursor{Rank: cursor.Rank, CreatedAt: cursor.CreatedAt.UTC(), ID: cursor.ID}, nil
}

// invalidRankedCursor builds the validation error for a rejected cursor.
func invalidRankedCursor(reason string) error {
	return &stories.ValidationError{Field: "cursor", Message: reason}
}

// isCanonicalUUID reports whether id is lowercase hexadecimal UUID text with
// hyphens in the canonical positions. It mirrors the stories package's check and
// exists so a cursor id is guaranteed to be safe for a uuid cast.
func isCanonicalUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for index := 0; index < len(id); index++ {
		character := id[index]
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

// rankedEntryID is the id a cursor stores: the adaptation's own id for a version
// entry, the story's id otherwise.
func rankedEntryID(item relevance.RankedStory) string {
	if item.VersionID != "" {
		return item.VersionID
	}
	return item.StoryID
}

// rankedAfter reports whether a candidate sorts strictly after the cursor in the
// ranker's total order (score descending, then newest first, then id ascending).
func rankedAfter(item relevance.RankedStory, cursor rankedCursor) bool {
	if item.Score != cursor.Rank {
		return item.Score < cursor.Rank
	}
	if !item.CreatedAt.Equal(cursor.CreatedAt) {
		return item.CreatedAt.Before(cursor.CreatedAt)
	}
	return rankedEntryID(item) > cursor.ID
}

// rankedIndexAfter answers how many ranked items to skip to resume after cursor.
//
// It first looks for the entry the cursor names. That match is on **id and
// creation time, not on the rank**: the recency term is measured against the
// current clock, so the same entry scores a few thousandths lower on the next
// request and an exact-score match would fail on every page turn. The id is
// unique within the window, so the position is unambiguous.
//
// If the entry is no longer in the window (new content pushed it out, or the
// corpus changed), the fallback skips everything that now sorts at or before the
// cursor's rank, so the page still starts near the reader's position instead of
// repeating the whole window. A ranking that changes *materially* between pages
// (new engagement, new content) can still reorder entries around the cursor;
// the contract is "no repeats while the ranking is unchanged", not a snapshot
// (KNOT-027, disclosed).
func rankedIndexAfter(ranked []relevance.RankedStory, cursor rankedCursor) int {
	for index, item := range ranked {
		if rankedEntryID(item) == cursor.ID && item.CreatedAt.Equal(cursor.CreatedAt) {
			return index + 1
		}
	}

	for index, item := range ranked {
		if rankedAfter(item, cursor) {
			return index
		}
	}

	return len(ranked)
}

// candidateWindow collects up to want of the newest entries.
//
// The stories service caps one page at `stories.MaxListLimit` (50), so the
// window is built from consecutive chronological pages: ranking more entries
// than a page must not mean asking the service for an illegal limit. The pages
// are keyset-exclusive, so the window never repeats an entry. Four sequential
// index-backed reads fill the default 200-entry window; the cost is disclosed in
// KNOT-027 rather than hidden in a larger service limit, because widening the
// public limit would change what every feed client may request.
func (h *StoriesHandler) candidateWindow(ctx context.Context, want int) ([]stories.FeedItem, error) {
	window := make([]stories.FeedItem, 0, want)
	cursor := ""

	for len(window) < want {
		size := want - len(window)
		if size > stories.MaxListLimit {
			size = stories.MaxListLimit
		}

		page, next, err := h.service.ListFeed(ctx, cursor, size)
		if err != nil {
			return nil, err
		}

		window = append(window, page...)
		if next == "" || len(page) == 0 {
			break
		}
		cursor = next
	}

	return window, nil
}

// candidateFor projects one feed entry onto the ranker's candidate shape,
// preferring the entity_features row and falling back to the entry's own fields
// when the read model has no row yet.
func candidateFor(entry stories.FeedItem, features map[string]stories.EntityFeature) relevance.StoryCandidate {
	if entry.Kind == stories.FeedKindVersion {
		version := entry.Version
		candidate := relevance.StoryCandidate{
			StoryID:   version.StoryID,
			VersionID: version.ID,
			AuthorID:  version.AuthorID,
			Language:  version.Language,
			Pillar:    string(version.Pillar),
			Place:     version.ApproximateLocation,
			CreatedAt: version.CreatedAt,
		}
		applyFeature(&candidate, features[version.ID])
		return candidate
	}

	candidate := relevance.StoryCandidate{
		StoryID:   entry.ID,
		AuthorID:  entry.AuthorID,
		Language:  entry.Language,
		Pillar:    string(entry.Pillar),
		Place:     entry.ApproximateLocation,
		CreatedAt: entry.CreatedAt,
	}
	applyFeature(&candidate, features[entry.ID])
	return candidate
}

// applyFeature overlays the read model's non-empty values on a candidate. A null
// column must not erase a value the feed query already supplied.
func applyFeature(candidate *relevance.StoryCandidate, feature stories.EntityFeature) {
	if feature.Language != "" {
		candidate.Language = feature.Language
	}
	if feature.Pillar != "" {
		candidate.Pillar = feature.Pillar
	}
	if feature.PlaceLower != "" {
		candidate.Place = feature.PlaceLower
	}
}

// feedEntryID is the id used to key a feed entry when mapping a ranking back to
// the entries it ranked: a version's own id, or the story's id.
func feedEntryID(entry stories.FeedItem) string {
	if entry.Kind == stories.FeedKindVersion {
		return entry.Version.ID
	}
	return entry.ID
}

// rankedFeedPage answers one page of the reader's ranked feed.
//
// It fetches a bounded chronological window, enriches the candidates with the
// entity_features read model in one batched query, ranks them with the
// deterministic SimpleRanker, and slices out the requested page. The window is
// independent of the cursor on purpose: a ranked cursor is a position in the
// *same* ordered window, so page two re-ranks the window and skips past the
// cursor rather than ranking only the unseen remainder (which would not be
// comparable). The cursor is therefore user-specific and stable while the
// window, the reader's profile and their engagement are unchanged.
func (h *StoriesHandler) rankedFeedPage(ctx context.Context, userID, rawCursor string, limit int) ([]stories.FeedItem, string, error) {
	if !h.ranking.enabled() {
		return nil, "", fmt.Errorf("httpapi: for_you feed requires ranking dependencies")
	}

	var after *rankedCursor
	if rawCursor != "" {
		cursor, err := decodeRankedCursor(rawCursor)
		if err != nil {
			return nil, "", err
		}
		after = &cursor
	}

	profile, err := h.ranking.Profiles.UserByID(ctx, userID)
	if err != nil {
		return nil, "", fmt.Errorf("httpapi: load feed profile: %w", err)
	}

	engagement, err := h.ranking.Engagement.PillarEngagement(ctx, userID)
	if err != nil {
		return nil, "", fmt.Errorf("httpapi: load feed engagement: %w", err)
	}

	window, err := h.candidateWindow(ctx, rankedCandidateWindow)
	if err != nil {
		return nil, "", err
	}

	ids := make([]string, 0, len(window))
	for _, entry := range window {
		ids = append(ids, feedEntryID(entry))
	}

	features, err := h.ranking.Features.FeaturesFor(ctx, ids)
	if err != nil {
		return nil, "", fmt.Errorf("httpapi: load feed features: %w", err)
	}

	byID := make(map[string]stories.FeedItem, len(window))
	candidates := make([]relevance.StoryCandidate, 0, len(window))
	for _, entry := range window {
		byID[feedEntryID(entry)] = entry
		candidates = append(candidates, candidateFor(entry, features))
	}

	ranker := relevance.NewSimpleRanker(
		relevance.UserProfile{PreferredLanguages: profile.PreferredLanguages},
		relevance.EngagementSummary{WeightedByPillar: engagement},
	)

	ranked, err := ranker.Rank(ctx, userID, candidates)
	if err != nil {
		return nil, "", fmt.Errorf("httpapi: rank feed: %w", err)
	}

	start := 0
	if after != nil {
		start = rankedIndexAfter(ranked, *after)
	}

	end := start + limit
	if end > len(ranked) {
		end = len(ranked)
	}
	page := ranked[start:end]

	items := make([]stories.FeedItem, 0, len(page))
	for _, item := range page {
		if entry, ok := byID[rankedEntryID(item)]; ok {
			items = append(items, entry)
		}
	}

	next := ""
	if end < len(ranked) && len(page) > 0 {
		next = encodeRankedCursor(page[len(page)-1])
	}

	return items, next, nil
}
