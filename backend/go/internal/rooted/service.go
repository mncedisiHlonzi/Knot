package rooted

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Service holds the Rooted business rules.
//
// It depends on the RootedStore abstraction and knows nothing about HTTP, JSON, or
// SQL.
type Service struct {
	store RootedStore
}

// NewService wires a store into the Rooted domain.
func NewService(store RootedStore) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("rooted: service requires a rooted store")
	}
	return &Service{store: store}, nil
}

// SetSignal validates the input and replaces the user's primary signal.
//
// A user has exactly one primary signal, so a second call replaces the first
// rather than adding another. It returns a *ValidationError for bad input and
// ErrUserNotFound when the user does not exist.
func (s *Service) SetSignal(ctx context.Context, userID string, in SetSignalInput) (Signal, error) {
	if !isUUID(userID) {
		return Signal{}, ErrUserNotFound
	}

	signal, err := validateSetSignal(in)
	if err != nil {
		return Signal{}, err
	}

	stored, err := s.store.SetPrimary(ctx, userID, signal)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return Signal{}, ErrUserNotFound
		}
		return Signal{}, fmt.Errorf("rooted: set signal: %w", err)
	}

	return stored, nil
}

// GetMySignals returns every signal for a user, public and private.
//
// This is the owner's own view, so a hidden signal is included. It never returns
// nil for an empty result.
func (s *Service) GetMySignals(ctx context.Context, userID string) ([]Signal, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	signals, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("rooted: list signals: %w", err)
	}

	return nonNilSignals(signals), nil
}

// GetPublicSignals returns a user's public signals, or ErrUserNotFound when the
// user does not exist.
//
// The existence check is what lets the public read answer 404 for an unknown user
// rather than an empty list, which would be indistinguishable from a user who has
// declared nothing.
func (s *Service) GetPublicSignals(ctx context.Context, userID string) ([]Signal, error) {
	if !isUUID(userID) {
		return nil, ErrUserNotFound
	}

	exists, err := s.store.UserExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("rooted: check user: %w", err)
	}
	if !exists {
		return nil, ErrUserNotFound
	}

	signals, err := s.store.ListPublicByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("rooted: list public signals: %w", err)
	}

	return nonNilSignals(signals), nil
}

// GetPrimaryPublicSignal returns the user's primary public signal, or nil when the
// user has none (or the id is not a UUID).
//
// A missing signal is not an error: it is the ordinary state of a user who has not
// declared a Rooted signal, or who has hidden it.
func (s *Service) GetPrimaryPublicSignal(ctx context.Context, userID string) (*Signal, error) {
	if !isUUID(userID) {
		return nil, nil
	}

	signals, err := s.store.BatchPrimaryPublic(ctx, []string{userID})
	if err != nil {
		return nil, fmt.Errorf("rooted: read primary public signal: %w", err)
	}

	signal, ok := signals[userID]
	if !ok {
		return nil, nil
	}

	// A copy, so callers never hold a pointer into map storage they could mutate.
	primary := signal
	return &primary, nil
}

// BatchGetPrimaryPublicSignals returns the primary public signal for each of the
// given user ids, keyed by user id, in a single query.
//
// It is how the HTTP layer enriches content responses without an N+1 query: the
// handler gathers the distinct author ids in a response and asks once. Ids that
// are empty or not canonical UUIDs are ignored, and a user with no primary public
// signal is simply absent from the map. The result is never nil.
func (s *Service) BatchGetPrimaryPublicSignals(ctx context.Context, userIDs []string) (map[string]*Signal, error) {
	unique := uniqueUUIDs(userIDs)
	out := make(map[string]*Signal, len(unique))
	if len(unique) == 0 {
		return out, nil
	}

	signals, err := s.store.BatchPrimaryPublic(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("rooted: batch primary public signals: %w", err)
	}

	for userID, signal := range signals {
		// A copy per entry, so every pointer in the map addresses its own value.
		primary := signal
		out[userID] = &primary
	}

	return out, nil
}

// validateSetSignal applies every input rule and returns the signal to store.
//
// The place is trimmed because surrounding whitespace is never meaningful. It is
// rejected when it contains a line break or a tab, so a "place" cannot smuggle in
// a multi-line address. Length is counted in runes so a multi-byte script is not
// penalised.
func validateSetSignal(in SetSignalInput) (Signal, error) {
	place := strings.TrimSpace(in.Place)

	if utf8.RuneCountInString(place) < MinPlaceLength {
		return Signal{}, &ValidationError{Field: "place", Message: "is required"}
	}
	if strings.ContainsAny(place, "\n\r\t") {
		return Signal{}, &ValidationError{Field: "place", Message: "must be a single line with no tabs"}
	}
	if utf8.RuneCountInString(place) > MaxPlaceLength {
		return Signal{}, &ValidationError{
			Field:   "place",
			Message: fmt.Sprintf("must be at most %d characters", MaxPlaceLength),
		}
	}

	if !in.DurationBucket.Valid() {
		return Signal{}, &ValidationError{
			Field:   "duration_bucket",
			Message: "must be one of lifelong, many_years, several_years, a_few_years, recently",
		}
	}

	latitude, longitude, err := validateCoordinates(in.Latitude, in.Longitude)
	if err != nil {
		return Signal{}, err
	}

	country := strings.TrimSpace(in.PlaceCountry)
	if utf8.RuneCountInString(country) > MaxPlaceCountryLength {
		return Signal{}, &ValidationError{
			Field:   "place_country",
			Message: fmt.Sprintf("must be at most %d characters", MaxPlaceCountryLength),
		}
	}
	var placeCountry *string
	if country != "" {
		placeCountry = &country
	}

	return Signal{
		Place:          place,
		Latitude:       latitude,
		Longitude:      longitude,
		PlaceCountry:   placeCountry,
		DurationBucket: in.DurationBucket,
		IsPublic:       in.IsPublic,
		IsPrimary:      true,
	}, nil
}

// validateCoordinates enforces that a coordinate is supplied as a pair, with each
// component in range, and returns the values to store.
//
// It duplicates the stories domain's helper rather than sharing one: the two live
// in different bounded contexts and the helper is a few lines of range checking
// (KNOT-ADR-010). A lone latitude or longitude is rejected, and supplying neither
// is valid and yields two nils.
func validateCoordinates(latitude, longitude *float64) (*float64, *float64, error) {
	if latitude == nil && longitude == nil {
		return nil, nil, nil
	}
	if latitude == nil || longitude == nil {
		return nil, nil, &ValidationError{
			Field:   "latitude",
			Message: "latitude and longitude must be provided together",
		}
	}

	if math.IsNaN(*latitude) || *latitude < -90 || *latitude > 90 {
		return nil, nil, &ValidationError{Field: "latitude", Message: "must be between -90 and 90"}
	}
	if math.IsNaN(*longitude) || *longitude < -180 || *longitude > 180 {
		return nil, nil, &ValidationError{Field: "longitude", Message: "must be between -180 and 180"}
	}

	return latitude, longitude, nil
}

// uniqueUUIDs returns the distinct canonical UUIDs in the order they first appear,
// dropping empty and malformed ids so a bad id can never reach the database.
func uniqueUUIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if !isUUID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}

	return out
}

// nonNilSignals guarantees a non-nil slice so a JSON response always contains an
// array rather than null.
func nonNilSignals(signals []Signal) []Signal {
	if signals == nil {
		return []Signal{}
	}
	return signals
}
