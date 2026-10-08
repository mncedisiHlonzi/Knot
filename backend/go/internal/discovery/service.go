package discovery

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/knot/backend/internal/stories"
)

// Service holds the discovery business rules.
//
// It depends on the DiscoveryStore abstraction and knows nothing about HTTP,
// JSON, or SQL.
type Service struct {
	store DiscoveryStore
}

// NewService wires a store into the discovery domain.
func NewService(store DiscoveryStore) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("discovery: service requires a discovery store")
	}
	return &Service{store: store}, nil
}

// ListClusters returns one entry per place, ordered by story count descending.
//
// The filter is validated and normalised before it reaches the store: the pillar
// must be one of the supported pillars, the language must be a simple tag, and
// the limit must be within the package's bounds. A cluster list is always
// returned, never nil, so the HTTP layer never has to map nil onto an empty
// array.
func (s *Service) ListClusters(ctx context.Context, filter ClusterFilter) ([]PlaceCluster, error) {
	normalized, err := validateClusterFilter(filter)
	if err != nil {
		return nil, err
	}

	clusters, err := s.store.ListClusters(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("discovery: list clusters: %w", err)
	}

	if clusters == nil {
		clusters = []PlaceCluster{}
	}

	return clusters, nil
}

// ListStoriesAtPlace returns one page of the stories at a place, newest first,
// plus the cursor that resumes after it.
//
// The place is matched on its normalised form: it is trimmed and lower-cased
// before the store sees it, so the caller's original casing does not matter. An
// empty rawCursor asks for the first page. The returned next cursor is the empty
// string when the page is the last one, mirroring the feed, so the HTTP layer
// signals "no more pages" without inventing a null-versus-absent distinction.
//
// A place with no stories is not an error: the store returns an empty page, and
// the caller answers 200 with an empty array.
func (s *Service) ListStoriesAtPlace(ctx context.Context, place string, rawCursor string, limit int) ([]stories.Story, string, error) {
	placeLower := strings.ToLower(strings.TrimSpace(place))
	if placeLower == "" {
		return nil, "", &ValidationError{Field: "place", Message: "is required"}
	}

	if limit < 1 || limit > MaxPlaceLimit {
		return nil, "", &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxPlaceLimit),
		}
	}

	var cursor *stories.Cursor
	if rawCursor != "" {
		decoded, err := stories.DecodeCursor(rawCursor)
		if err != nil {
			// The stories cursor error is deliberately not propagated: its type
			// belongs to another package. A malformed cursor is reported here as a
			// discovery validation error on the same field name.
			return nil, "", &ValidationError{
				Field:   "cursor",
				Message: "must be a base64url-encoded token issued by this API",
			}
		}
		cursor = &decoded
	}

	page, next, err := s.store.ListStoriesAtPlace(ctx, placeLower, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("discovery: list stories at place: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never has to special-case an empty
		// place.
		page = []stories.Story{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// validateClusterFilter applies every filter rule and returns the filter to pass
// to the store. It never mutates the caller's value.
func validateClusterFilter(filter ClusterFilter) (ClusterFilter, error) {
	if filter.Pillar != "" && !filter.Pillar.Valid() {
		return ClusterFilter{}, &ValidationError{
			Field:   "pillar",
			Message: fmt.Sprintf("must be one of %s, %s", stories.PillarWonder, stories.PillarHeritage),
		}
	}

	language := strings.ToLower(strings.TrimSpace(filter.Language))
	if language != "" && !isLanguageTag(language) {
		return ClusterFilter{}, &ValidationError{Field: "language", Message: "must be 2 to 8 letters"}
	}

	if filter.Limit < 1 || filter.Limit > MaxClusterLimit {
		return ClusterFilter{}, &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxClusterLimit),
		}
	}

	return ClusterFilter{Pillar: filter.Pillar, Language: language, Limit: filter.Limit}, nil
}

// isLanguageTag reports whether s is a 2-8 letter ASCII language tag. It mirrors
// the stories domain's own rule so a language accepted on a story is accepted
// when filtering discovery by it.
func isLanguageTag(s string) bool {
	if utf8.RuneCountInString(s) < 2 || utf8.RuneCountInString(s) > 8 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return true
}
