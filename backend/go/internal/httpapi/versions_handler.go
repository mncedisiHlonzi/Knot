package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/knot/backend/internal/versions"
)

// VersionsService is the slice of the versions service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database.
type VersionsService interface {
	CreateAdaptation(ctx context.Context, in versions.CreateAdaptationInput) (versions.StoryVersion, error)
	GetVersion(ctx context.Context, id string) (versions.StoryVersion, error)
	GetTree(ctx context.Context, storyID string) ([]versions.StoryVersion, error)
}

// VersionsHandler serves the Tell My People endpoints.
type VersionsHandler struct {
	service VersionsService
	rooted  RootedLookup
	logger  *slog.Logger
}

// NewVersionsHandler returns a handler backed by service. The rooted lookup is
// used to attach each author's inline Rooted summary to a version response.
func NewVersionsHandler(service VersionsService, rooted RootedLookup, logger *slog.Logger) (*VersionsHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a service")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a rooted lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: versions handler requires a logger")
	}
	return &VersionsHandler{service: service, rooted: rooted, logger: logger}, nil
}

// createAdaptationRequest is the POST /stories/{id}/adapt body.
//
// There is deliberately no author_id field. The adapter is taken from the
// authenticated request, so a client cannot adapt as somebody else — and because
// decodeJSON rejects unknown fields, sending author_id is a 400 rather than a
// silently ignored field.
type createAdaptationRequest struct {
	ParentVersionID string `json:"parent_version_id"`
	Language        string `json:"language"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	AdaptationNote  string `json:"adaptation_note"`
}

// versionResponse is the public projection of a story version. It is an explicit
// type, not the domain StoryVersion, so the wire format is a deliberate choice.
//
// ParentVersionID and AdaptationNote are pointers so an absent value is JSON
// null: the root version has no parent, and an adapter may leave no note.
type versionResponse struct {
	ID              string    `json:"id"`
	StoryID         string    `json:"story_id"`
	ParentVersionID *string   `json:"parent_version_id"`
	AuthorID        string    `json:"author_id"`
	Language        string    `json:"language"`
	Title           string    `json:"title"`
	Body            string    `json:"body"`
	AdaptationNote  *string   `json:"adaptation_note"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// AuthorRooted is the version author's primary public Rooted signal, or null
	// when they have none. It is a summary (place and duration only), attached at
	// the HTTP layer; see KNOT-ADR-017.
	AuthorRooted *rootedSummary `json:"author_rooted"`
}

// versionEnvelope wraps a single version, so the response shape can gain sibling
// fields later without breaking clients.
type versionEnvelope struct {
	Version versionResponse `json:"version"`
}

// treeResponse is the GET /stories/{id}/tree body. Versions is a flat adjacency
// list: every version carries its parent pointer and the client assembles the
// tree. Versions is always an array, never null.
type treeResponse struct {
	StoryID  string            `json:"story_id"`
	Versions []versionResponse `json:"versions"`
}

// Adapt handles POST /stories/{id}/adapt. The route is protected, so the adapter
// id comes from the context rather than from the body.
func (h *VersionsHandler) Adapt(w http.ResponseWriter, r *http.Request) {
	authorID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept
		// as a fail-closed guard so that a future wiring mistake cannot create a
		// version with no author.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createAdaptationRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateAdaptation(r.Context(), versions.CreateAdaptationInput{
		StoryID:         r.PathValue("id"),
		ParentVersionID: body.ParentVersionID,
		AuthorID:        authorID,
		Language:        body.Language,
		Title:           body.Title,
		Body:            body.Body,
		AdaptationNote:  body.AdaptationNote,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "story or parent version not found")
		return
	}

	response := newVersionResponse(created)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{created.AuthorID})[created.AuthorID]

	writeJSON(w, http.StatusCreated, versionEnvelope{Version: response})
}

// Tree handles GET /stories/{id}/tree. The route is public.
func (h *VersionsHandler) Tree(w http.ResponseWriter, r *http.Request) {
	storyID := r.PathValue("id")

	list, err := h.service.GetTree(r.Context(), storyID)
	if err != nil {
		h.writeServiceError(w, r, err, "story not found")
		return
	}

	authorIDs := make([]string, 0, len(list))
	for _, version := range list {
		authorIDs = append(authorIDs, version.AuthorID)
	}
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)

	items := make([]versionResponse, 0, len(list))
	for _, version := range list {
		item := newVersionResponse(version)
		item.AuthorRooted = summaries[version.AuthorID]
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, treeResponse{StoryID: storyID, Versions: items})
}

// Get handles GET /versions/{id}. The route is public.
func (h *VersionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	version, err := h.service.GetVersion(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "version not found")
		return
	}

	response := newVersionResponse(version)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{version.AuthorID})[version.AuthorID]

	writeJSON(w, http.StatusOK, versionEnvelope{Version: response})
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a
// generic 500 and is logged with the request id. notFoundMessage lets each route
// name what was not found.
func (h *VersionsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	var validation *versions.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, versions.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage)
	default:
		h.logger.ErrorContext(
			r.Context(),
			"versions request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// newVersionResponse projects a domain version onto the wire format.
func newVersionResponse(version versions.StoryVersion) versionResponse {
	return versionResponse{
		ID:              version.ID,
		StoryID:         version.StoryID,
		ParentVersionID: optionalString(version.ParentVersionID),
		AuthorID:        version.AuthorID,
		Language:        version.Language,
		Title:           version.Title,
		Body:            version.Body,
		AdaptationNote:  optionalString(version.AdaptationNote),
		CreatedAt:       version.CreatedAt,
		UpdatedAt:       version.UpdatedAt,
	}
}

// optionalString maps "" onto nil so an absent value is serialised as JSON null
// rather than as an empty string that a client might mistake for a real value.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
