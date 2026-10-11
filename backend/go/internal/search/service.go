package search

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/knot/backend/internal/language"
	"github.com/knot/backend/internal/stories"
)

// MaxQueryLength bounds the search phrase. It is generous for a search box and
// exists so an oversized phrase cannot be turned into an oversized tsquery.
const MaxQueryLength = 200

// MaxPlaceLength bounds the place filter. It matches the bound the rest of Knot
// puts on a place name, so a filter cannot carry an address.
const MaxPlaceLength = 100

// Service holds the search business rules.
//
// It depends on the SearchStore abstraction and knows nothing about HTTP, JSON, or
// SQL.
type Service struct {
	store SearchStore
}

// NewService wires a store into the search domain.
func NewService(store SearchStore) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("search: service requires a search store")
	}
	return &Service{store: store}, nil
}

// Search returns one page of ranked results for in, plus the opaque cursor that
// resumes after it ("" when this page is the last one).
//
// Every rule is applied here before the store sees anything: the phrase is trimmed
// and length-checked, the enums are validated, the author id is proven to be
// canonical UUID text, and the cursor is decoded. A page is always returned, never
// nil, so the HTTP layer never has to map nil onto an empty array.
//
// A phrase and a filter are not alternatives: the phrase narrows the filter (all
// English stories matching "manguzi"). A request with neither is refused, because
// it would return the whole corpus in relevance-free order.
func (s *Service) Search(ctx context.Context, in SearchInput) ([]Result, string, error) {
	query, err := validateSearchInput(in)
	if err != nil {
		return nil, "", err
	}

	page, next, err := s.store.Search(ctx, query)
	if err != nil {
		return nil, "", fmt.Errorf("search: search: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never special-cases an empty result.
		page = []Result{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// validateSearchInput applies every rule and returns the query to pass to the
// store. It never mutates the caller's value.
func validateSearchInput(in SearchInput) (StoreQuery, error) {
	phrase := strings.TrimSpace(in.Query)
	place := strings.TrimSpace(in.Place)
	code := strings.TrimSpace(in.Language)
	authorID := strings.TrimSpace(in.AuthorID)
	pillar := stories.Pillar(strings.TrimSpace(in.Pillar))

	length := utf8.RuneCountInString(phrase)
	if phrase != "" && length < MinQueryLength {
		return StoreQuery{}, &ValidationError{
			Field:   "q",
			Message: fmt.Sprintf("must be at least %d characters", MinQueryLength),
		}
	}
	if length > MaxQueryLength {
		return StoreQuery{}, &ValidationError{
			Field:   "q",
			Message: fmt.Sprintf("must be at most %d characters", MaxQueryLength),
		}
	}

	if in.Kind != "" && !in.Kind.Valid() {
		return StoreQuery{}, &ValidationError{
			Field:   "type",
			Message: fmt.Sprintf("must be one of %s, %s, %s", KindStory, KindVersion, KindInquiry),
		}
	}

	if pillar != "" && !pillar.Valid() {
		return StoreQuery{}, &ValidationError{
			Field:   "pillar",
			Message: fmt.Sprintf("must be one of %s, %s", stories.PillarWonder, stories.PillarHeritage),
		}
	}

	if code != "" && !language.IsValid(code) {
		return StoreQuery{}, &ValidationError{
			Field:   "language",
			Message: "must be a valid ISO 639-3 language code, such as eng or zul",
		}
	}

	if authorID != "" && !isUUID(authorID) {
		return StoreQuery{}, &ValidationError{
			Field:   "author_id",
			Message: "must be a UUID",
		}
	}

	if utf8.RuneCountInString(place) > MaxPlaceLength {
		return StoreQuery{}, &ValidationError{
			Field:   "place",
			Message: fmt.Sprintf("must be at most %d characters", MaxPlaceLength),
		}
	}

	// A search with no phrase needs at least one filter to bound it. Without either,
	// the request would scan and return everything.
	if phrase == "" && in.Kind == "" && pillar == "" && code == "" && authorID == "" && place == "" {
		return StoreQuery{}, &ValidationError{
			Field:   "q",
			Message: "is required when no filter is provided",
		}
	}

	limit := in.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return StoreQuery{}, &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxLimit),
		}
	}

	// A cursor is optional. A malformed one is the client's error, so the decode
	// error is returned as-is rather than rewrapped.
	query := StoreQuery{
		Query:    phrase,
		Kind:     in.Kind,
		Pillar:   string(pillar),
		Language: code,
		AuthorID: authorID,
		Place:    place,
		Limit:    limit,
	}

	if in.Cursor != "" {
		decoded, err := DecodeCursor(in.Cursor)
		if err != nil {
			return StoreQuery{}, err
		}
		query.Cursor = &decoded
	}

	return query, nil
}
