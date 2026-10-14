package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/discovery"
	"github.com/knot/backend/internal/stories"
)

// DiscoveryService is the slice of the discovery service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database.
type DiscoveryService interface {
	ListClusters(ctx context.Context, filter discovery.ClusterFilter) ([]discovery.PlaceCluster, error)
	ListStoriesAtPlace(ctx context.Context, place string, country string, pillar string, cursor string, limit int) ([]stories.Story, string, error)
}

// DiscoveryHandler serves the discovery endpoints.
type DiscoveryHandler struct {
	service   DiscoveryService
	authors   AuthorLookup
	rooted    RootedLookup
	media     StoryMediaLookup
	reactions ReactionsLookup
	stats     StoryStatsLookup
	logger    *slog.Logger
}

// NewDiscoveryHandler returns a handler backed by service. The lookups are the
// same enrichment dependencies the stories handler uses, so a place's stories are
// decorated identically to the feed's: the author lookup names and pictures each
// author, the rooted lookup attaches their inline Rooted summary, the media
// lookup attaches the single-item preview, the reactions lookup attaches the
// counts and the reader's own signals, and the stats lookup attaches the
// aggregate counts.
func NewDiscoveryHandler(service DiscoveryService, authors AuthorLookup, rooted RootedLookup, media StoryMediaLookup, reactions ReactionsLookup, stats StoryStatsLookup, logger *slog.Logger) (*DiscoveryHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires a service")
	}
	if authors == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires an author lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires a rooted lookup")
	}
	if media == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires a story media lookup")
	}
	if reactions == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires a reactions lookup")
	}
	if stats == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires a story stats lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: discovery handler requires a logger")
	}
	return &DiscoveryHandler{service: service, authors: authors, rooted: rooted, media: media, reactions: reactions, stats: stats, logger: logger}, nil
}

// clusterResponse is the public projection of a place cluster. It is an explicit
// type, not the domain PlaceCluster, so the wire format is a deliberate choice.
type clusterResponse struct {
	Place        string   `json:"place"`
	PlaceCountry *string  `json:"place_country"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	StoryCount   int      `json:"story_count"`
	// PillarCounts is keyed by pillar; both pillars are always present.
	PillarCounts  map[string]int `json:"pillar_counts"`
	Languages     []string       `json:"languages"`
	LatestStoryAt time.Time      `json:"latest_story_at"`
}

// clustersResponse is the GET /discovery/clusters body. Clusters is always an
// array, never null.
type clustersResponse struct {
	Clusters []clusterResponse `json:"clusters"`
}

// placeStoriesResponse is the GET /discovery/places/{place} body. The stories
// are the same projection the feed uses, so a client renders them identically,
// and NextCursor is the empty string on the last page.
type placeStoriesResponse struct {
	Stories    []storyResponse `json:"stories"`
	NextCursor string          `json:"next_cursor"`
}

// Clusters handles GET /discovery/clusters. The route is public.
//
// Any combination of pillar, language, and limit may be present; an absent filter
// is simply not applied, and an out-of-range limit is clamped by parseClusterLimit
// before the service sees it.
func (h *DiscoveryHandler) Clusters(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseClusterLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	clusters, err := h.service.ListClusters(r.Context(), discovery.ClusterFilter{
		Pillar:   stories.Pillar(query.Get("pillar")),
		Language: query.Get("language"),
		Limit:    limit,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, clustersResponse{Clusters: newClusterResponses(clusters)})
}

// PlaceStories handles GET /discovery/places/{place}. The route is public.
//
// The place comes from the path, URL-decoded by the router, so the caller may use
// any casing: the service normalises it before matching. A place with no stories
// is a 200 with an empty array, not a 404, so a client can tell "no stories here"
// from "this endpoint is wrong".
//
// The optional `country` query parameter narrows the page to one country, which
// is how a place cluster — keyed by (place, country) — is opened without also
// pulling in the same-named places in other countries. Its reserved value "null"
// asks for the stories that record no country at all. An absent parameter lists
// every story at the place, which is what the endpoint did before KNOT-020e-fix2.
//
// The optional `pillar` query parameter narrows the page to one storytelling
// lens, which is what the place screen's All / Heritage / Wonder tabs send. It
// must be one of the supported pillars; anything else is a 400, because a pillar
// is a closed set rather than free text. An absent parameter lists every pillar
// (KNOT-020e-fix6).
//
// Each story is projected onto the same response the feed uses and enriched
// through the same helper, so a story carries identical fields — author name and
// avatar, inline Rooted summary, media preview, aggregate counts, and reactions —
// whether it is read from the feed or from a place.
func (h *DiscoveryHandler) PlaceStories(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parsePlaceLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	page, next, err := h.service.ListStoriesAtPlace(
		r.Context(),
		r.PathValue("place"),
		query.Get("country"),
		query.Get("pillar"),
		query.Get("cursor"),
		limit,
	)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	items := make([]storyResponse, 0, len(page))
	for _, story := range page {
		items = append(items, newStoryResponse(story))
	}

	// The same batched enrichment the feed applies, through the same helper, so a
	// story reads identically in both places (KNOT-020e-fix2).
	storyPageLookups{
		authors:   h.authors,
		rooted:    h.rooted,
		media:     h.media,
		reactions: h.reactions,
		stats:     h.stats,
		logger:    h.logger,
	}.enrich(r.Context(), items)

	writeJSON(w, http.StatusOK, placeStoriesResponse{Stories: items, NextCursor: next})
}

// parseClusterLimit reads the optional "limit" query parameter for a cluster
// list. An absent limit means DefaultClusterLimit; a limit above MaxClusterLimit
// is clamped, and one that is not a positive integer is a validation failure.
func parseClusterLimit(raw string) (int, error) {
	if raw == "" {
		return discovery.DefaultClusterLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &discovery.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &discovery.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > discovery.MaxClusterLimit {
		return discovery.MaxClusterLimit, nil
	}

	return value, nil
}

// parsePlaceLimit reads the optional "limit" query parameter for a place page.
// It follows the same rules as parseClusterLimit with the place page's bounds.
func parsePlaceLimit(raw string) (int, error) {
	if raw == "" {
		return discovery.DefaultPlaceLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &discovery.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &discovery.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > discovery.MaxPlaceLimit {
		return discovery.MaxPlaceLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Discovery has no
// not-found case — an unknown place is an empty result, not a 404 — so a
// validation failure is the only client-visible error, and everything else
// becomes a generic 500 logged with the request id.
func (h *DiscoveryHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *discovery.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	default:
		h.logger.ErrorContext(
			r.Context(),
			"discovery request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// newClusterResponses projects a slice of domain clusters, always emitting an
// array rather than null.
func newClusterResponses(clusters []discovery.PlaceCluster) []clusterResponse {
	out := make([]clusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, newClusterResponse(cluster))
	}
	return out
}

// newClusterResponse projects a domain cluster onto the wire format. Pillar
// counts are keyed by the pillar's string value, and languages is never null.
func newClusterResponse(cluster discovery.PlaceCluster) clusterResponse {
	pillarCounts := make(map[string]int, len(cluster.PillarCounts))
	for pillar, count := range cluster.PillarCounts {
		pillarCounts[string(pillar)] = count
	}

	languages := cluster.Languages
	if languages == nil {
		languages = []string{}
	}

	return clusterResponse{
		Place:         cluster.Place,
		PlaceCountry:  cluster.PlaceCountry,
		Latitude:      cluster.Latitude,
		Longitude:     cluster.Longitude,
		StoryCount:    cluster.StoryCount,
		PillarCounts:  pillarCounts,
		Languages:     languages,
		LatestStoryAt: cluster.LatestStoryAt,
	}
}
