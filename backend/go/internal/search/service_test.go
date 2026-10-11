package search

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeStore records the query it received and returns a canned page, so the
// service rules can be tested without a database.
type fakeStore struct {
	page     []Result
	next     *Cursor
	err      error
	gotQuery StoreQuery
	calls    int
}

func (f *fakeStore) Search(_ context.Context, query StoreQuery) ([]Result, *Cursor, error) {
	f.calls++
	f.gotQuery = query
	return f.page, f.next, f.err
}

// newTestService returns a service over a fake store.
func newTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()

	store := &fakeStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, store
}

// TestNewServiceRejectsNilStore proves the constructor refuses a missing
// dependency, which is a programming error rather than a runtime condition.
func TestNewServiceRejectsNilStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Error("NewService(nil) error = nil, want an error")
	}
}

// TestSearchRejectsInvalidInput proves every validation rule names the offending
// field, so a handler can answer a 400 a client can act on.
func TestSearchRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input SearchInput
		field string
	}{
		{
			name:  "a one-character phrase is too short",
			input: SearchInput{Query: "a"},
			field: "q",
		},
		{
			name:  "a phrase counted in runes is not measured in bytes",
			input: SearchInput{Query: "é"},
			field: "q",
		},
		{
			name:  "an over-long phrase is refused",
			input: SearchInput{Query: strings.Repeat("z", MaxQueryLength+1)},
			field: "q",
		},
		{
			name:  "an unknown type is refused",
			input: SearchInput{Query: "manguzi", Kind: Kind("stories")},
			field: "type",
		},
		{
			name:  "an unknown pillar is refused",
			input: SearchInput{Query: "manguzi", Pillar: "curiosity"},
			field: "pillar",
		},
		{
			name:  "a two-letter language is refused",
			input: SearchInput{Query: "manguzi", Language: "en"},
			field: "language",
		},
		{
			name:  "an unknown language is refused",
			input: SearchInput{Query: "manguzi", Language: "qqq"},
			field: "language",
		},
		{
			name:  "a non-uuid author is refused",
			input: SearchInput{Query: "manguzi", AuthorID: "ada"},
			field: "author_id",
		},
		{
			name:  "an over-long place is refused",
			input: SearchInput{Query: "manguzi", Place: strings.Repeat("p", MaxPlaceLength+1)},
			field: "place",
		},
		{
			name:  "a limit above the cap is refused",
			input: SearchInput{Query: "manguzi", Limit: MaxLimit + 1},
			field: "limit",
		},
		{
			name:  "a negative limit is refused",
			input: SearchInput{Query: "manguzi", Limit: -1},
			field: "limit",
		},
		{
			name:  "no phrase and no filter is refused",
			input: SearchInput{},
			field: "q",
		},
		{
			name:  "a malformed cursor is refused",
			input: SearchInput{Query: "manguzi", Cursor: "not-a-cursor"},
			field: "cursor",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store := newTestService(t)

			_, _, err := service.Search(context.Background(), test.input)
			if err == nil {
				t.Fatalf("Search(%+v) error = nil, want an error", test.input)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Search(%+v) error = %v, want a *ValidationError", test.input, err)
			}
			if validation.Field != test.field {
				t.Errorf("validation field = %q, want %q", validation.Field, test.field)
			}
			if store.calls != 0 {
				t.Errorf("store was called %d times for a rejected request, want 0", store.calls)
			}
		})
	}
}

// TestSearchAllowsFiltersWithoutPhrase proves a filter-only search is valid: the
// phrase may be empty when something else bounds the query.
func TestSearchAllowsFiltersWithoutPhrase(t *testing.T) {
	filters := []SearchInput{
		{Kind: KindInquiry},
		{Pillar: "wonder"},
		{Language: "eng"},
		{AuthorID: "11111111-1111-4111-8111-111111111111"},
		{Place: "Manguzi"},
	}

	for _, input := range filters {
		service, store := newTestService(t)

		if _, _, err := service.Search(context.Background(), input); err != nil {
			t.Fatalf("Search(%+v) error = %v, want nil", input, err)
		}
		if store.gotQuery.Query != "" {
			t.Errorf("store query = %q, want empty for a filter-only search", store.gotQuery.Query)
		}
	}
}

// TestSearchNormalisesAndDefaults proves the service trims the values, resolves the
// default page size, and preserves a valid kind.
func TestSearchNormalisesAndDefaults(t *testing.T) {
	service, store := newTestService(t)

	if _, _, err := service.Search(context.Background(), SearchInput{
		Query:    "  Manguzi  ",
		Kind:     KindVersion,
		Pillar:   "  heritage  ",
		Language: " zul ",
		AuthorID: " 11111111-1111-4111-8111-111111111111 ",
		Place:    "  Durban  ",
	}); err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	got := store.gotQuery
	if got.Query != "Manguzi" {
		t.Errorf("Query = %q, want %q", got.Query, "Manguzi")
	}
	if got.Kind != KindVersion {
		t.Errorf("Kind = %q, want %q", got.Kind, KindVersion)
	}
	if got.Pillar != "heritage" {
		t.Errorf("Pillar = %q, want %q", got.Pillar, "heritage")
	}
	if got.Language != "zul" {
		t.Errorf("Language = %q, want %q", got.Language, "zul")
	}
	if got.AuthorID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("AuthorID = %q, want the trimmed uuid", got.AuthorID)
	}
	if got.Place != "Durban" {
		t.Errorf("Place = %q, want %q", got.Place, "Durban")
	}
	if got.Limit != DefaultLimit {
		t.Errorf("Limit = %d, want %d", got.Limit, DefaultLimit)
	}
	if got.Cursor != nil {
		t.Errorf("Cursor = %v, want nil", got.Cursor)
	}
}

// TestSearchDecodesCursor proves a valid cursor is decoded before it reaches the
// store, so the store never handles the wire form.
func TestSearchDecodesCursor(t *testing.T) {
	service, store := newTestService(t)

	const id = "22222222-2222-4222-8222-222222222222"
	created := time.Date(2026, 10, 4, 11, 0, 0, 0, time.UTC)
	cursor := NewCursor(0.5, created, id).Encode()

	if _, _, err := service.Search(context.Background(), SearchInput{Query: "manguzi", Cursor: cursor}); err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	if store.gotQuery.Cursor == nil {
		t.Fatal("store Cursor = nil, want the decoded cursor")
	}
	if store.gotQuery.Cursor.ID() != id {
		t.Errorf("store cursor id = %q, want %q", store.gotQuery.Cursor.ID(), id)
	}
	if !store.gotQuery.Cursor.CreatedAt().Equal(created) {
		t.Errorf("store cursor createdAt = %v, want %v", store.gotQuery.Cursor.CreatedAt(), created)
	}
}

// TestSearchEncodesNextCursor proves the store's cursor is rendered opaque for the
// wire, and that no cursor becomes the empty string rather than a null.
func TestSearchEncodesNextCursor(t *testing.T) {
	created := time.Date(2026, 10, 4, 11, 0, 0, 0, time.UTC)
	next := NewCursor(0.25, created, "33333333-3333-4333-8333-333333333333")

	service, store := newTestService(t)
	store.next = &next

	_, encoded, err := service.Search(context.Background(), SearchInput{Query: "manguzi"})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if encoded != next.Encode() {
		t.Errorf("next cursor = %q, want %q", encoded, next.Encode())
	}

	service, store = newTestService(t)
	store.next = nil

	_, encoded, err = service.Search(context.Background(), SearchInput{Query: "manguzi"})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if encoded != "" {
		t.Errorf("next cursor = %q, want empty on the last page", encoded)
	}
}

// TestSearchNeverReturnsNilPage proves an empty result is an empty slice, so the
// JSON body carries [] rather than null.
func TestSearchNeverReturnsNilPage(t *testing.T) {
	service, store := newTestService(t)
	store.page = nil

	page, _, err := service.Search(context.Background(), SearchInput{Query: "manguzi"})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if page == nil {
		t.Fatal("page = nil, want an empty slice")
	}
	if len(page) != 0 {
		t.Errorf("page = %d results, want 0", len(page))
	}
}

// TestSearchWrapsStoreError proves a store failure is wrapped with %w so the cause
// stays inspectable, rather than being flattened into a string.
func TestSearchWrapsStoreError(t *testing.T) {
	service, store := newTestService(t)
	store.err = errStoreFailure

	_, _, err := service.Search(context.Background(), SearchInput{Query: "manguzi"})
	if err == nil {
		t.Fatal("Search() error = nil, want an error")
	}
	if !errors.Is(err, errStoreFailure) {
		t.Errorf("errors.Is(err, errStoreFailure) = false, want true (error = %v)", err)
	}
	if !strings.Contains(err.Error(), "search") {
		t.Errorf("error = %q, want it to mention the package", err.Error())
	}
}

// errStoreFailure stands in for a database failure.
var errStoreFailure = errors.New("connection reset")
