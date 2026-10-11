package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/search"
)

// SearchService is the slice of the search service that the HTTP layer needs.
// Depending on an interface (rather than the concrete service) keeps handler tests
// free of a database.
type SearchService interface {
	Search(ctx context.Context, in search.SearchInput) ([]search.Result, string, error)
}

// SearchHandler serves the search endpoint.
type SearchHandler struct {
	service SearchService
	authors AuthorLookup
	rooted  RootedLookup
	logger  *slog.Logger
}

// NewSearchHandler returns a handler backed by service. Attribution and Rooted
// summaries are attached with the same batched lookups every other content handler
// uses, so a search result names and pictures its author exactly as a feed card
// does (KNOT-ADR-041).
func NewSearchHandler(service SearchService, authors AuthorLookup, rooted RootedLookup, logger *slog.Logger) (*SearchHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: search handler requires a service")
	}
	if authors == nil {
		return nil, fmt.Errorf("httpapi: search handler requires an author lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: search handler requires a rooted lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: search handler requires a logger")
	}
	return &SearchHandler{service: service, authors: authors, rooted: rooted, logger: logger}, nil
}

// searchResultResponse is the public projection of one ranked result.
//
// It is an explicit type, not the domain Result, so the wire format is a
// deliberate choice: the domain's Rank is pagination state and never leaves the
// server.
type searchResultResponse struct {
	Kind    search.Kind `json:"kind"`
	ID      string      `json:"id"`
	StoryID *string     `json:"story_id"`
	Title   string      `json:"title"`
	Preview string      `json:"preview"`
	// Language is the ISO 639-3 code the matched text is written in.
	Language string `json:"language"`
	// Pillar is the story's pillar, or null for an inquiry.
	Pillar *string `json:"pillar"`
	// Place is the story's place or the inquiry's place, or null when none was
	// recorded.
	Place    *string `json:"place"`
	AuthorID string  `json:"author_id"`
	// AuthorDisplayName and AuthorAvatarURL are the author's inline attribution,
	// filled by one batched lookup per response (KNOT-ADR-041).
	AuthorDisplayName string         `json:"author_display_name"`
	AuthorAvatarURL   *string        `json:"author_avatar_url"`
	AuthorRooted      *rootedSummary `json:"author_rooted"`
	CreatedAt         time.Time      `json:"created_at"`
}

// searchResponse is the GET /search body. Results is always an array, never null.
type searchResponse struct {
	Results    []searchResultResponse `json:"results"`
	NextCursor string                 `json:"next_cursor"`
}

// Search handles GET /search. The route is public and runs through optional auth.
//
// Every parameter is optional on its own; the service refuses a request that has
// neither a phrase nor a filter, because that would return the whole corpus. The
// block filter is not handled here: it is read from the request context inside the
// store, so an authenticated reader's hidden authors never reach the ranking
// (KNOT-ADR-064).
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseSearchLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	results, next, err := h.service.Search(r.Context(), search.SearchInput{
		Query:    query.Get("q"),
		Kind:     search.Kind(query.Get("type")),
		Pillar:   query.Get("pillar"),
		Language: query.Get("language"),
		AuthorID: query.Get("author_id"),
		Place:    query.Get("place"),
		Cursor:   query.Get("cursor"),
		Limit:    limit,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	authorIDs := make([]string, 0, len(results))
	for _, result := range results {
		authorIDs = append(authorIDs, result.AuthorID)
	}

	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)

	items := make([]searchResultResponse, 0, len(results))
	for _, result := range results {
		item := searchResultResponse{
			Kind:         result.Kind,
			ID:           result.ID,
			StoryID:      result.StoryID,
			Title:        result.Title,
			Preview:      result.Preview,
			Language:     result.Language,
			Pillar:       result.Pillar,
			Place:        result.Place,
			AuthorID:     result.AuthorID,
			CreatedAt:    result.CreatedAt,
			AuthorRooted: summaries[result.AuthorID],
		}
		item.AuthorDisplayName = attributions[result.AuthorID].DisplayName
		item.AuthorAvatarURL = optionalString(attributions[result.AuthorID].AvatarURL)

		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, searchResponse{Results: items, NextCursor: next})
}

// parseSearchLimit reads the optional "limit" query parameter. An absent limit
// means DefaultLimit; a limit above MaxLimit is clamped, and one that is not a
// positive integer is a validation failure.
func parseSearchLimit(raw string) (int, error) {
	if raw == "" {
		return search.DefaultLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &search.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &search.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > search.MaxLimit {
		return search.MaxLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise are translated; anything else is logged and reported as a 500, so an
// internal failure is never dressed up as a client error.
func (h *SearchHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *search.ValidationError

	if errors.As(err, &validation) {
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
		return
	}

	h.logger.ErrorContext(
		r.Context(),
		"search request failed",
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)
	writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
}
