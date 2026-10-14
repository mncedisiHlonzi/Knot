package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/knot/backend/internal/language"
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
// must be one of the supported pillars, the language must be a canonical ISO
// 639-3 code, and the limit must be within the package's bounds. A cluster list
// is always returned, never nil, so the HTTP layer never has to map nil onto an
// empty array.
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
// before the store sees it, so the caller's original casing does not matter. The
// optional rawCountry narrows the page to one country — the one a cluster was
// keyed by — and is interpreted by parsePlaceCountry. An empty rawCursor asks for
// the first page. The returned next cursor is the empty string when the page is
// the last one, mirroring the feed, so the HTTP layer signals "no more pages"
// without inventing a null-versus-absent distinction.
//
// A place with no stories is not an error: the store returns an empty page, and
// the caller answers 200 with an empty array.
//
// An empty page is not the same as rejecting the input: an unrecognised country
// simply matches nothing, exactly as an unknown place does, so there is no
// validation failure to report and no way to enumerate another place's countries.
func (s *Service) ListStoriesAtPlace(ctx context.Context, place string, rawCountry string, rawCursor string, limit int) ([]stories.Story, string, error) {
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

	page, next, err := s.store.ListStoriesAtPlace(ctx, placeLower, parsePlaceCountry(rawCountry), cursor, limit)
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

// placeCountryAbsentToken is the `?country=` value that asks for the stories
// which record no country. The word "null" is reserved for this purpose, so a
// country literally named "null" cannot be matched by name (KNOT-020e-fix2).
const placeCountryAbsentToken = "null"

// parsePlaceCountry turns the raw `?country=` query value into a store filter.
//
// An absent or blank value does not narrow the page at all — the "every story at
// this place" case. The sentinel "null" (any casing, after trimming) asks for the
// stories that record no country, which is how a country-less cluster's page is
// opened. Anything else is the exact country to match.
//
// There is no validation failure: an unknown country is a valid filter that
// matches nothing, exactly as an unknown place does.
func parsePlaceCountry(raw string) PlaceCountryFilter {
	value := strings.TrimSpace(raw)

	switch {
	case value == "":
		return PlaceCountryFilter{}
	case strings.EqualFold(value, placeCountryAbsentToken):
		return PlaceCountryFilter{Absent: true}
	default:
		return PlaceCountryFilter{Name: value}
	}
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

	// An empty language means "do not filter by language"; a non-empty one must be
	// a canonical ISO 639-3 code, matched exactly, so discovery accepts exactly the
	// codes a story can be authored in (KNOT-ADR-046).
	code := strings.TrimSpace(filter.Language)
	if code != "" && !language.IsValid(code) {
		return ClusterFilter{}, &ValidationError{Field: "language", Message: "must be a valid ISO 639-3 language code, such as eng or zul"}
	}

	if filter.Limit < 1 || filter.Limit > MaxClusterLimit {
		return ClusterFilter{}, &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxClusterLimit),
		}
	}

	return ClusterFilter{Pillar: filter.Pillar, Language: code, Limit: filter.Limit}, nil
}
