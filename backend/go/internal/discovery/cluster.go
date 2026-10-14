// Package discovery implements the Knot discovery domain: finding stories by
// place.
//
// Discovery does two things. It groups stories into **place clusters** — one
// entry per place, carrying how many stories are there, which pillars they
// belong to, which languages they are told in, and when the newest one was
// published — and it lists the stories at a single place, newest first, with the
// same opaque cursor pagination the feed uses.
//
// The package is deliberately layered, mirroring internal/stories:
//
//	cluster.go        domain types, limits, errors, and the store contract
//	service.go        business rules (ListClusters, ListStoriesAtPlace)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Nothing in this package knows about HTTP, JSON, or SQL types. A place is
// identified by its normalised name together with the country the geocoder
// reported (KNOT-020e-fix): two stories about "Beijing" are one cluster however
// far apart their coordinates, and the cluster's own coordinate is the average of
// its members' points, used to place the map marker. Discovery itself never
// geocodes; it only groups what the stories write path stored. The package
// reuses the stories domain's Story type and its opaque pagination cursor rather
// than defining parallel ones, because a story at a place is an ordinary story
// read through a different query.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/knot/backend/internal/stories"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field, so
	// errors.Is(err, ErrValidation) and errors.As(err, &validation) are both true
	// for the same value.
	ErrValidation = errors.New("discovery: invalid input")
)

// ValidationError describes a rejected field on a request. Handlers expose the
// Field and Message so clients can highlight the offending input.
type ValidationError struct {
	// Field is the request field that failed validation.
	Field string
	// Message explains the failure in a client-safe way.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("discovery: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Cluster and place page sizes. The service enforces them so the domain, not
// just the HTTP layer, refuses an unbounded query.
const (
	// DefaultClusterLimit is the cluster page size the API uses when none is
	// requested. A world map of places is small, so the default is generous.
	DefaultClusterLimit = 100
	// MaxClusterLimit is the largest cluster page the API will serve.
	MaxClusterLimit = 500
	// DefaultPlaceLimit is the place story page size the API uses when none is
	// requested. It matches the feed's default so a place reads like the feed.
	DefaultPlaceLimit = 20
	// MaxPlaceLimit is the largest place story page the API will serve.
	MaxPlaceLimit = 50
)

// ClusterFilter narrows which places ListClusters aggregates.
//
// The zero value is valid and means "every place, unfiltered", with Limit
// resolved by the service to DefaultClusterLimit.
type ClusterFilter struct {
	// Pillar, when set, keeps only places that have at least one story filed
	// under that pillar. An empty value does not filter.
	Pillar stories.Pillar
	// Language, when set, keeps only places that have at least one version
	// written in that language tag. An empty value does not filter.
	Language string
	// Limit bounds how many clusters are returned.
	Limit int
}

// PlaceCountryFilter narrows which stories a place page lists by the country
// they record.
//
// The zero value applies no country filter, so the page lists every story at the
// place whatever country each records. A cluster, though, is keyed by
// (place, country) (KNOT-020e-fix), so opening one asks for exactly its members:
// either a named country, or — with Absent set — the stories that record no
// country at all. This is what keeps a country-scoped cluster's story count and
// its page's list length in agreement (KNOT-020e-fix2).
type PlaceCountryFilter struct {
	// Name is the exact country to match, compared verbatim because the caller
	// passes back the value a cluster reported.
	Name string
	// Absent selects the stories whose place_country is NULL. It takes precedence
	// over Name.
	Absent bool
}

// Enabled reports whether the filter narrows the page at all. The zero value is
// disabled, which is the "every story at this place" case.
func (f PlaceCountryFilter) Enabled() bool { return f.Absent || f.Name != "" }

// PlaceCluster is one place with the stories told there.
//
// It is an aggregate, not a row: Place is a representative spelling of the
// place name, and the remaining fields summarise every story grouped under it.
type PlaceCluster struct {
	// Place is the place name as the earliest member story spelled it, with the
	// author's capitalisation preserved. Grouping itself is by the normalised lower
	// case form together with PlaceCountry (KNOT-020e-fix).
	Place string
	// PlaceCountry is the country the geocoder reported for the place, or nil for a
	// legacy cluster that predates structured place data. It is part of the
	// cluster's identity: a story with no country is its own cluster, never merged
	// with a same-named place that has one.
	PlaceCountry *string
	// Latitude and Longitude are the average of the member stories' coordinates, so
	// the marker sits among the stories rather than on one arbitrary member. The
	// pair is nil (together) for a cluster whose members carry no coordinate — a
	// legacy cluster, or one whose stories were all told by name.
	Latitude  *float64
	Longitude *float64
	// StoryCount is how many stories are at this place.
	StoryCount int
	// PillarCounts is the story count per pillar. Both supported pillars are
	// always present, so a place with only wonder stories still carries a
	// heritage entry of zero.
	PillarCounts map[stories.Pillar]int
	// Languages is the distinct, sorted set of languages in which stories at
	// this place are told. It is never nil.
	Languages []string
	// LatestStoryAt is the creation time of the newest story at this place.
	LatestStoryAt time.Time
}

// DiscoveryStore is the persistence contract for discovery. The service depends
// on this interface rather than on pgx, so the business rules can be tested
// without a database.
type DiscoveryStore interface {
	// ListClusters returns one entry per place, keyed by the normalised place name
	// and country, ordered by story count descending, applying filter. The result
	// is never nil.
	ListClusters(ctx context.Context, filter ClusterFilter) ([]PlaceCluster, error)
	// ListStoriesAtPlace returns at most limit stories at the already-normalised
	// place, newest first, starting after cursor. A nil cursor starts at the
	// newest story. country narrows the page to the stories a cluster with that
	// country holds, and pillar to the stories filed under one storytelling lens;
	// each zero value does not narrow. The returned cursor is the position to
	// resume from, or nil when the page is the last one.
	ListStoriesAtPlace(ctx context.Context, placeLower string, country PlaceCountryFilter, pillar stories.Pillar, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error)
}
