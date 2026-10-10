package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
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
	ListStories(ctx context.Context, cursor string, limit int) ([]stories.Story, string, error)
}

// StoriesHandler serves the story endpoints.
type StoriesHandler struct {
	service   StoriesService
	authors   AuthorLookup
	rooted    RootedLookup
	media     StoryMediaLookup
	reactions ReactionsLookup
	logger    *slog.Logger
}

// NewStoriesHandler returns a handler backed by service. The author lookup names and
// pictures each story's author (one batched read per response), the rooted lookup
// attaches that author's inline Rooted summary, the media lookup attaches the
// story's media (in full on the detail, as a single preview on the feed), and the
// reactions lookup attaches the perspective-reaction counts and the reader's own
// signals.
func NewStoriesHandler(service StoriesService, authors AuthorLookup, rooted RootedLookup, media StoryMediaLookup, reactions ReactionsLookup, logger *slog.Logger) (*StoriesHandler, error) {
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
	if logger == nil {
		return nil, fmt.Errorf("httpapi: stories handler requires a logger")
	}
	return &StoriesHandler{service: service, authors: authors, rooted: rooted, media: media, reactions: reactions, logger: logger}, nil
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
	Sensitive           bool           `json:"sensitive"`
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
	Sensitive    bool      `json:"sensitive"`
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
}

// storyEnvelope wraps a single story, so the response shape can gain sibling
// fields later without breaking clients.
type storyEnvelope struct {
	Story storyResponse `json:"story"`
}

// listStoriesResponse is the GET /stories body. NextCursor is the empty string
// on the last page; the field is always present so a client can test it without
// distinguishing absent from empty.
type listStoriesResponse struct {
	Stories    []storyResponse `json:"stories"`
	NextCursor string          `json:"next_cursor"`
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
		Sensitive:           body.Sensitive,
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

	userID, _ := UserIDFromContext(r.Context())
	response.Reactions, response.MyReactions = reactionsForEntity(r.Context(), h.reactions, h.logger, reactions.EntityStory, userID, story.ID)

	writeJSON(w, http.StatusOK, storyEnvelope{Story: response})
}

// List handles GET /stories. The route is public.
func (h *StoriesHandler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	page, next, err := h.service.ListStories(r.Context(), query.Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	items := make([]storyResponse, 0, len(page))
	ids := make([]string, 0, len(page))
	authorIDs := make([]string, 0, len(page))
	for _, story := range page {
		items = append(items, newStoryResponse(story))
		ids = append(ids, story.ID)
		authorIDs = append(authorIDs, story.AuthorID)
	}

	// One batched read names and pictures every author on the page.
	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	for i := range items {
		attribution := attributions[items[i].AuthorID]
		items[i].AuthorDisplayName = attribution.DisplayName
		items[i].AuthorAvatarURL = optionalString(attribution.AvatarURL)
	}

	previews := storyMediaPreviews(r.Context(), h.media, h.logger, ids)
	for i := range items {
		if preview, ok := previews[items[i].ID]; ok {
			items[i].Media = preview
		}
	}

	// One batched pair of reads decorates the whole page with reaction counts and
	// the reader's own signals (KNOT-ADR-051).
	readerID, _ := UserIDFromContext(r.Context())
	reactionData := loadReactions(r.Context(), h.reactions, h.logger, reactions.EntityStory, readerID, ids)
	for i := range items {
		items[i].Reactions = reactionData.Counts[items[i].ID]
		items[i].MyReactions = orEmptyStrings(reactionData.Mine[items[i].ID])
	}

	writeJSON(w, http.StatusOK, listStoriesResponse{Stories: items, NextCursor: next})
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
		Sensitive:           story.Sensitive,
		CreatedAt:           story.CreatedAt,
		UpdatedAt:           story.UpdatedAt,
		// Emit [] rather than null until the enrichment fills it in.
		Media:       []storyMediaResponse{},
		MyReactions: []string{},
	}
}
