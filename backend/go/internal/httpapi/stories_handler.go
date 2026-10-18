package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// codeUnauthorized is returned when a protected route is reached without a valid
// access token.
const codeUnauthorized = "unauthorized"

// codeNotFound is returned when a requested resource does not exist.
const codeNotFound = "not_found"

// StoriesService is the slice of the stories service that the HTTP layer needs.
// Depending on an interface (rather than the concrete service) keeps handler
// tests free of a database.
type StoriesService interface {
	CreateStory(ctx context.Context, in stories.CreateStoryInput) (stories.Story, error)
	GetStory(ctx context.Context, id string) (stories.Story, error)
	ListFeed(ctx context.Context, cursor string, limit int) ([]stories.FeedItem, string, error)
}

// The feed's discriminator values. They mirror the domain's own literals so the
// wire format cannot drift from what the store writes into the union.
const (
	feedKindStory   = string(stories.FeedKindStory)
	feedKindVersion = string(stories.FeedKindVersion)
)

// StoriesHandler serves the story endpoints.
type StoriesHandler struct {
	service   StoriesService
	authors   AuthorLookup
	rooted    RootedLookup
	media     StoryMediaLookup
	reactions ReactionsLookup
	stats     StoryStatsLookup
	chains    VersionChainLookup
	logger    *slog.Logger
}

// NewStoriesHandler returns a handler backed by service. The author lookup names and
// pictures each story's author (one batched read per response), the rooted lookup
// attaches that author's inline Rooted summary, the media lookup attaches the
// story's media (in full on the detail, as a single preview on the feed), the
// reactions lookup attaches the perspective-reaction counts and the reader's own
// signals, the stats lookup attaches the story's aggregate counts (versions,
// languages, comments, media) in one batched read per response, and the chain
// lookup attributes the adaptations a feed page carries (KNOT-022).
func NewStoriesHandler(service StoriesService, authors AuthorLookup, rooted RootedLookup, media StoryMediaLookup, reactions ReactionsLookup, stats StoryStatsLookup, chains VersionChainLookup, logger *slog.Logger) (*StoriesHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a service")
	}
	if authors == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires an author lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a rooted lookup")
	}
	if media == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a story media lookup")
	}
	if reactions == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a reactions lookup")
	}
	if stats == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a story stats lookup")
	}
	if chains == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a version chain lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a logger")
	}
	return &StoriesHandler{service: service, authors: authors, rooted: rooted, media: media, reactions: reactions, stats: stats, chains: chains, logger: logger}, nil
}

// createStoryRequest is the POST /stories body.
//
// There is deliberately no author_id field. The author is taken from the
// authenticated request, so a client cannot publish as somebody else — and
// because decodeJSON rejects unknown fields, sending author_id is a 400 rather
// than a silently ignored field.
type createStoryRequest struct {
	Pillar              stories.Pillar `json:"pillar"`
	Language            string         `json:"language"`
	Title               string         `json:"title"`
	Body                string         `json:"body"`
	ApproximateLocation string         `json:"approximate_location"`
	Latitude            *float64       `json:"latitude"`
	Longitude           *float64       `json:"longitude"`
	PlaceCountry        string         `json:"place_country"`
	MediaURLs           []string       `json:"media_urls"`
}

// storyResponse is the public projection of a story. It is an explicit type, not
// the domain Story, so the wire format is a deliberate choice rather than
// whatever the domain type happens to expose.
//
// The content fields (language, title, body) are the story's root version
// content, resolved from story_versions. root_version_id names that version, so
// a client can adapt the story from the root without a second lookup.
type storyResponse struct {
	ID       string `json:"id"`
	AuthorID string `json:"author_id"`
	// AuthorDisplayName and AuthorAvatarURL are the author's inline attribution,
	// filled by one batched lookup per response. AuthorAvatarURL is the backend
	// path to fetch an avatar from, or null when the author has none, so a client
	// renders initials (KNOT-ADR-041).
	AuthorDisplayName   string         `json:"author_display_name"`
	AuthorAvatarURL     *string        `json:"author_avatar_url"`
	RootVersionID       string         `json:"root_version_id"`
	Pillar              stories.Pillar `json:"pillar"`
	Language            string         `json:"language"`
	Title               string         `json:"title"`
	Body                string         `json:"body"`
	ApproximateLocation string         `json:"approximate_location"`
	// Latitude, Longitude, and PlaceCountry are the structured place data. The
	// coordinate pair is null together or set together (KNOT-ADR-034).
	Latitude     *float64  `json:"latitude"`
	Longitude    *float64  `json:"longitude"`
	PlaceCountry *string   `json:"place_country"`
	MediaURLs    []string  `json:"media_urls"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// AuthorRooted is the author's primary public Rooted signal, or null when they
	// have none. It is a summary (place and duration only), attached at the HTTP
	// layer; see KNOT-ADR-017.
	AuthorRooted *rootedSummary `json:"author_rooted"`
	// Media is the story's attached media, never null. On the story detail it is
	// the full list in display order; on the feed it is a single-item preview (or
	// an empty array) so a card can show a thumbnail.
	Media []storyMediaResponse `json:"media"`
	// Reactions is the count of each of the four perspective signals on the story,
	// always all four keys (KNOT-ADR-051). MyReactions is the signals the
	// authenticated reader holds, as an array of reaction types ([] when signed
	// out or when they hold none), so the client can highlight them.
	Reactions   reactionCountsResponse `json:"reactions"`
	MyReactions []string               `json:"my_reactions"`
	// VersionCount, LanguageCount, CommentCount, and MediaCount are the story's
	// aggregate counts, attached in one batched read (KNOT-020c-fix). Zero is a
	// real answer (no comments yet), so they are ints rather than pointers and are
	// always present, letting a feed entry render its meta row without a second
	// request.
	VersionCount  int `json:"version_count"`
	LanguageCount int `json:"language_count"`
	CommentCount  int `json:"comment_count"`
	MediaCount    int `json:"media_count"`
	// FirstMediaType and FirstMediaURL describe the story's first media item (in
	// display order), or are null when it has none, so the feed can render an
	// image, a video placeholder, or nothing without walking the media array
	// (KNOT-020c-fix).
	FirstMediaType *string `json:"first_media_type"`
	FirstMediaURL  *string `json:"first_media_url"`
	// FirstMediaWidth and FirstMediaHeight are that item's pixel dimensions, or
	// null when the uploader's client did not report them. They let a list frame
	// the media at its natural width:height ratio instead of a fixed one, so a
	// portrait photo is not cropped; a consumer must fall back to its own default
	// ratio when either is null (KNOT-020g-fix5).
	FirstMediaWidth  *int `json:"first_media_width"`
	FirstMediaHeight *int `json:"first_media_height"`
	// PreviewTruncated is true when the story's body is longer than the preview
	// window a feed entry shows, so the entry can offer "read more" instead of
	// ending on a bare ellipsis. The feed still carries the whole body — clamping
	// it to three lines is the client's job — so this is the server's answer to
	// "is there more here than a preview shows", and it keeps the client from
	// inferring truncation from the text (KNOT-020g-fix12).
	PreviewTruncated bool `json:"preview_truncated"`
	// AttributionLine is the root version's attribution sentence for a story,
	// and VersionDepth is how many adaptations separate it from its original --
	// always 0, because a story IS its root version (KNOT-021). Both are
	// pointers so that `omitempty` drops them from an entry whose attribution is
	// carried elsewhere: a story entry in the feed omits them, because a story
	// card is already attributed by its author line, while an ADAPTATION entry
	// carries its own `attribution_line` (see feedVersionItem). A client must
	// treat them as absent when they are missing (KNOT-022).
	AttributionLine *string `json:"attribution_line,omitempty"`
	VersionDepth    *int    `json:"version_depth,omitempty"`
}

// storyEnvelope wraps a single story, so the response shape can gain sibling
// fields later without breaking clients.
type storyEnvelope struct {
	Story storyResponse `json:"story"`
}

// feedStoryItem is a story entry in the feed: the story projection plus the
// discriminator a client switches on.
//
// storyResponse is embedded by value so every story field stays flat on the wire
// and the story shape keeps exactly one definition. kind is the only addition,
// and the embedded struct has no field of that name, so nothing is shadowed
// (KNOT-022).
type feedStoryItem struct {
	Kind string `json:"kind"`
	storyResponse
}

// feedVersionItem is an ADAPTATION entry in the feed: one version somebody wrote
// of somebody else's story.
//
// It is the version-side counterpart of storyResponse, and it carries the same
// card-shaped fields (author line, framing, media preview, counts, reactions) so
// one feed component can render both kinds. What differs is identity: id is the
// VERSION, story_id is the story it adapts, and everything the version itself
// does not own -- place and media -- is inherited from that story, which is where
// those columns live (KNOT-ADR-030, KNOT-ADR-034).
//
// There is no version_depth here. A feed entry is never a root, so its depth is
// at least 1, and a card wants the number a reader says out loud ("this is the
// third version"), which is version_number = depth + 1 (KNOT-021, KNOT-022).
type feedVersionItem struct {
	Kind string `json:"kind"`
	// ID is the version's own id: it is what the reader opens, reacts to, and
	// follows from here.
	ID string `json:"id"`
	// StoryID is the story being adapted, so a client can open the original or
	// the language tree from the entry.
	StoryID string `json:"story_id"`
	// ParentVersionID is the version this one adapts. It is never null: a root
	// version is the story and is not a feed entry of its own, so every version
	// the feed carries has a parent (KNOT-022).
	ParentVersionID string `json:"parent_version_id"`
	// AuthorID is the ADAPTER, not the original author; the original is named in
	// AttributionLine.
	AuthorID string `json:"author_id"`
	// AuthorDisplayName and AuthorAvatarURL are the adapter's inline attribution,
	// filled by one batched lookup per response. AuthorAvatarURL is null when the
	// author has none, so a client renders initials (KNOT-ADR-041).
	AuthorDisplayName string         `json:"author_display_name"`
	AuthorAvatarURL   *string        `json:"author_avatar_url"`
	AuthorRooted      *rootedSummary `json:"author_rooted"`
	// RootAuthorID, RootAuthorDisplayName, and RootAuthorAvatarURL describe the
	// ORIGINAL author: the first link of the lineage. They let a card head an
	// adaptation with "{name}'s original story" from structured data instead of
	// parsing the attribution sentence (KNOT-023a). They are derived from the same
	// batched chain and author read the sentence uses, so they cost no extra query;
	// the avatar is null when the root author has none, and the name is empty when
	// the lineage could not be resolved.
	RootAuthorID          string  `json:"root_author_id"`
	RootAuthorDisplayName string  `json:"root_author_display_name"`
	RootAuthorAvatarURL   *string `json:"root_author_avatar_url"`
	Language              string  `json:"language"`
	Title                 string  `json:"title"`
	Body                  string  `json:"body"`
	// Pillar is the story's pillar, inherited: a pillar frames the story, and an
	// adaptation of a heritage story is still heritage.
	Pillar stories.Pillar `json:"pillar"`
	// ApproximateLocation, Latitude, Longitude, and PlaceCountry are the story's
	// place, inherited for the same reason -- a version carries no place of its
	// own in the data model.
	ApproximateLocation string    `json:"approximate_location"`
	Latitude            *float64  `json:"latitude"`
	Longitude           *float64  `json:"longitude"`
	PlaceCountry        *string   `json:"place_country"`
	MediaURLs           []string  `json:"media_urls"`
	CreatedAt           time.Time `json:"created_at"`
	// MediaCount and the FirstMedia* fields describe the STORY's media preview, so
	// an adaptation entry shows the story's pictures. The version's own text is
	// what the card shows as content, and the card renders media from these fields
	// rather than from a media array, so the entry carries no array.
	MediaCount       int                    `json:"media_count"`
	FirstMediaType   *string                `json:"first_media_type"`
	FirstMediaURL    *string                `json:"first_media_url"`
	FirstMediaWidth  *int                   `json:"first_media_width"`
	FirstMediaHeight *int                   `json:"first_media_height"`
	Reactions        reactionCountsResponse `json:"reactions"`
	MyReactions      []string               `json:"my_reactions"`
	// VersionCount, LanguageCount, and CommentCount describe the story, so the
	// entry can say how much adaptation has already happened to it.
	VersionCount  int `json:"version_count"`
	LanguageCount int `json:"language_count"`
	CommentCount  int `json:"comment_count"`
	// VersionNumber is the entry's position in its lineage, counting the original
	// as version 1. It is derived from the parent pointers at read time: a first
	// adaptation is version 2.
	VersionNumber int `json:"version_number"`
	// AttributionLine is the human sentence tracing this version back to the
	// original, rendered server-side; the client shows it verbatim. It is empty
	// only when the lineage could not be resolved, because attribution must never
	// fail a page (KNOT-021, KNOT-022).
	AttributionLine string `json:"attribution_line"`
	// PreviewTruncated answers "is there more of this version's body than a card
	// shows", the same contract a story entry follows (KNOT-020g-fix12).
	PreviewTruncated bool `json:"preview_truncated"`
}

// newFeedVersionItem projects a stored feed version onto the wire. Everything the
// row answers is filled here; the enrichment fills the rest.
func newFeedVersionItem(version stories.VersionFeedItem) feedVersionItem {
	media := version.MediaURLs
	if media == nil {
		// Emit [] rather than null for an empty list.
		media = []string{}
	}

	return feedVersionItem{
		Kind:                feedKindVersion,
		ID:                  version.ID,
		StoryID:             version.StoryID,
		ParentVersionID:     version.ParentVersionID,
		AuthorID:            version.AuthorID,
		Language:            version.Language,
		Title:               version.Title,
		Body:                version.Body,
		Pillar:              version.Pillar,
		ApproximateLocation: version.ApproximateLocation,
		Latitude:            version.Latitude,
		Longitude:           version.Longitude,
		PlaceCountry:        version.PlaceCountry,
		MediaURLs:           media,
		CreatedAt:           version.CreatedAt,
		PreviewTruncated:    bodyExceedsPreviewWindow(version.Body),
		// Emit [] rather than null until the enrichment fills it in.
		MyReactions: []string{},
	}
}

// listStoriesResponse is the GET /stories body.
//
// Items is the merged feed: story entries and adaptation entries in one
// newest-first stream, each carrying the "kind" that says which shape it is. The
// field is named items rather than stories because a page is no longer only
// stories (KNOT-022, KNOT-ADR-070); the concrete element types are feedStoryItem
// and feedVersionItem.
//
// NextCursor is the empty string on the last page; the field is always present so
// a client can test it without distinguishing absent from empty.
type listStoriesResponse struct {
	Items      []any  `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// Create handles POST /stories. The route is protected, so the author id comes
// from the context rather than from the body.
func (h *StoriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	authorID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept
		// as a fail-closed guard so that a future wiring mistake cannot publish
		// a story with no author.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createStoryRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateStory(r.Context(), stories.CreateStoryInput{
		AuthorID:            authorID,
		Latitude:            body.Latitude,
		Longitude:           body.Longitude,
		PlaceCountry:        body.PlaceCountry,
		Pillar:              body.Pillar,
		Language:            body.Language,
		Title:               body.Title,
		Body:                body.Body,
		ApproximateLocation: body.ApproximateLocation,
		MediaURLs:           body.MediaURLs,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	response := newStoryResponse(created)
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, created.AuthorID)

	writeJSON(w, http.StatusCreated, storyEnvelope{Story: response})
}

// Get handles GET /stories/{id}. The route is public.
func (h *StoriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	story, err := h.service.GetStory(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	response := newStoryResponse(story)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{story.AuthorID})[story.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, story.AuthorID)
	response.Media = storyMediaForDetail(r.Context(), h.media, h.logger, story.ID)
	attachStoryCounts(&response, h.stats, r, h.logger, story.ID)

	userID, _ := UserIDFromContext(r.Context())
	response.Reactions, response.MyReactions = reactionsForEntity(r.Context(), h.reactions, h.logger, reactions.EntityStory, userID, story.ID)

	// The story detail IS the root version, so its attribution needs no store
	// walk and no second author read: one link, holding the story's own author --
	// whose display name this response has already resolved for the author line,
	// so the two can never disagree -- and the language its root version is
	// written in, which the response also carries (KNOT-021).
	rootChain := versions.Chain{{
		VersionID:         story.RootVersionID,
		StoryID:           story.ID,
		AuthorID:          story.AuthorID,
		AuthorDisplayName: response.AuthorDisplayName,
		Language:          story.Language,
	}}
	response.AttributionLine = optionalString(versions.FormatAttribution(rootChain))
	response.VersionDepth = optionalInt(0)

	writeJSON(w, http.StatusOK, storyEnvelope{Story: response})
}

// List handles GET /stories. The route is public.
//
// The page is a merged stream of two entry kinds (KNOT-022). Each page is
// enriched in one batched read per lookup — author attribution, the author's
// inline Rooted summary, the media preview, the aggregate counts, and the
// reactions — so a page of entries is a fixed number of queries rather than one
// per row.
//
// The two kinds are collected as two groups so each entry keeps its identity, then
// enriched TOGETHER: the identity, Rooted, media, and count reads span both kinds
// in one call each, because the keys they need (the entry's author, and the story
// behind the entry) can be unioned without ambiguity. Reactions are the exception,
// which feedPageLookups documents. The enriched groups are then interleaved back
// into the order the store returned.
func (h *StoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	page, next, err := h.service.ListFeed(r.Context(), query.Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	storyItems := make([]storyResponse, 0, len(page))
	storySlots := make([]int, 0, len(page))
	versionItems := make([]feedVersionItem, 0, len(page))
	versionSlots := make([]int, 0, len(page))

	for i, entry := range page {
		if entry.Kind == stories.FeedKindVersion {
			versionItems = append(versionItems, newFeedVersionItem(entry.Version))
			versionSlots = append(versionSlots, i)
			continue
		}

		storyItems = append(storyItems, newStoryResponse(entry.Story))
		storySlots = append(storySlots, i)
	}

	feedPageLookups{
		authors:   h.authors,
		rooted:    h.rooted,
		media:     h.media,
		reactions: h.reactions,
		stats:     h.stats,
		chains:    h.chains,
		logger:    h.logger,
	}.enrich(r.Context(), storyItems, versionItems)

	items := make([]any, len(page))
	for i, slot := range storySlots {
		items[slot] = feedStoryItem{Kind: feedKindStory, storyResponse: storyItems[i]}
	}
	for i, slot := range versionSlots {
		items[slot] = versionItems[i]
	}

	writeJSON(w, http.StatusOK, listStoriesResponse{Items: items, NextCursor: next})
}

// parseLimit reads the optional "limit" query parameter.
//
// An absent limit means DefaultListLimit. A limit above MaxListLimit is clamped
// to it rather than rejected, so a client asking for "as many as you can" gets a
// usable page instead of an error. A limit that is not a positive integer is a
// validation failure: it is a malformed request, not an over-large one.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return stories.DefaultListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &stories.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &stories.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > stories.MaxListLimit {
		return stories.MaxListLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a
// generic 500 and is logged with the request id.
func (h *StoriesHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *stories.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, stories.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "story not found")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"stories request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// previewWindowChars is the number of body characters a preview shows.
//
// A feed entry draws the story's body under a three-line clamp, which on a phone
// is roughly 40 characters a line, so a body past 150 characters is comfortably
// longer than anything the clamp can show and there is certainly more to read.
// 150 is the same window the profile wall's SQL excerpt uses
// (profile.MaxPreviewChars), so the feed and a profile card agree about what "a
// preview" is (KNOT-020g-fix12).
const previewWindowChars = 150

// bodyExceedsPreviewWindow reports whether a body is longer than the preview
// window, so an entry can offer a "read more" link.
//
// It counts characters rather than bytes, matching the profile wall's SQL
// `length(body)`, so a multi-byte body is measured the same way on both paths.
func bodyExceedsPreviewWindow(body string) bool {
	return utf8.RuneCountInString(body) > previewWindowChars
}

// newStoryResponse projects a domain story onto the wire format.
func newStoryResponse(story stories.Story) storyResponse {
	media := story.MediaURLs
	if media == nil {
		// Emit [] rather than null for an empty list.
		media = []string{}
	}

	return storyResponse{
		ID:                  story.ID,
		AuthorID:            story.AuthorID,
		RootVersionID:       story.RootVersionID,
		Pillar:              story.Pillar,
		Language:            story.Language,
		Title:               story.Title,
		Body:                story.Body,
		ApproximateLocation: story.ApproximateLocation,
		Latitude:            story.Latitude,
		Longitude:           story.Longitude,
		PlaceCountry:        story.PlaceCountry,
		MediaURLs:           media,
		CreatedAt:           story.CreatedAt,
		UpdatedAt:           story.UpdatedAt,
		PreviewTruncated:    bodyExceedsPreviewWindow(story.Body),
		// Emit [] rather than null until the enrichment fills it in.
		Media:       []storyMediaResponse{},
		MyReactions: []string{},
	}
}

// attachStoryCounts attaches a single story's aggregate counts and its first
// media item's kind, URL, and dimensions, in one batched read (KNOT-020c-fix).
//
// It is the single-entity counterpart of the batched loop in List. The first
// media fields are read from the response's own media, so the detail and the
// feed agree on which item is first, and are null when the story has no media.
func attachStoryCounts(response *storyResponse, lookup StoryStatsLookup, r *http.Request, logger *slog.Logger, storyID string) {
	counts := storyStats(r.Context(), lookup, logger, []string{storyID})[storyID]
	response.VersionCount = counts.Versions
	response.LanguageCount = counts.Languages
	response.CommentCount = counts.Comments
	response.MediaCount = counts.Media

	first := firstMediaFields(response.Media)
	response.FirstMediaType = first.mediaType
	response.FirstMediaURL = first.url
	response.FirstMediaWidth = first.width
	response.FirstMediaHeight = first.height
}

// firstMedia is the projection of a story's first media item onto the fields
// that describe it: its kind, its relative URL, and its pixel dimensions.
//
// Every field is a pointer so "the story has no media" is a null on the wire
// rather than an empty string or a zero dimension. The dimensions are null when
// the uploader's device did not report them (KNOT-020g-fix5).
type firstMedia struct {
	mediaType *string
	url       *string
	width     *int
	height    *int
}

// firstMediaFields projects the first item of a story's media onto the wire
// fields that describe it, or the zero value when the story has no media.
//
// The dimensions let a list frame the media at its natural width:height ratio
// instead of a fixed one, so a portrait photo or video is not cropped
// (KNOT-020g-fix5). They are null when the uploader's client did not report
// them, so a consumer falls back to its own default ratio.
func firstMediaFields(media []storyMediaResponse) firstMedia {
	if len(media) == 0 {
		return firstMedia{}
	}

	kind := media[0].MediaType
	url := media[0].URL
	return firstMedia{
		mediaType: &kind,
		url:       &url,
		width:     media[0].Width,
		height:    media[0].Height,
	}
}
