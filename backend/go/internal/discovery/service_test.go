package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/knot/backend/internal/stories"
)

// testCursorTime and testCursorID are a valid (timestamp, id) pair for building
// cursors in tests.
var testCursorTime = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

const testCursorID = "11111111-1111-4111-8111-111111111111"

// fakeStore records what the service passed down and returns canned values, so
// the business rules can be tested without a database.
type fakeStore struct {
	clusters    []PlaceCluster
	clustersErr error

	page    []stories.Story
	next    *stories.Cursor
	listErr error

	gotFilter ClusterFilter
	gotPlace  string
	gotCursor *stories.Cursor
	gotLimit  int
}

func (f *fakeStore) ListClusters(_ context.Context, filter ClusterFilter) ([]PlaceCluster, error) {
	f.gotFilter = filter
	return f.clusters, f.clustersErr
}

func (f *fakeStore) ListStoriesAtPlace(_ context.Context, placeLower string, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error) {
	f.gotPlace = placeLower
	f.gotCursor = cursor
	f.gotLimit = limit
	return f.page, f.next, f.listErr
}

func newTestService(t *testing.T, store DiscoveryStore) *Service {
	t.Helper()
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}
	return service
}

func TestNewServiceRequiresStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Error("NewService(nil) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}); err != nil {
		t.Errorf("NewService(store) error = %v, want nil", err)
	}
}

func TestListClustersPassesFilterThrough(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store)

	_, err := service.ListClusters(context.Background(), ClusterFilter{
		Pillar:   stories.PillarWonder,
		Language: "eng",
		Limit:    25,
	})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}

	if store.gotFilter.Pillar != stories.PillarWonder {
		t.Errorf("pillar = %q, want %q", store.gotFilter.Pillar, stories.PillarWonder)
	}
	if store.gotFilter.Language != "eng" {
		t.Errorf("language = %q, want %q", store.gotFilter.Language, "eng")
	}
	if store.gotFilter.Limit != 25 {
		t.Errorf("limit = %d, want 25", store.gotFilter.Limit)
	}
}

func TestListClustersTrimsLanguageButRequiresACanonicalCode(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store)

	if _, err := service.ListClusters(context.Background(), ClusterFilter{Language: " eng ", Limit: 10}); err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if store.gotFilter.Language != "eng" {
		t.Errorf("language = %q, want %q (trimmed)", store.gotFilter.Language, "eng")
	}

	// The filter is matched exactly, like every other language field.
	if _, err := service.ListClusters(context.Background(), ClusterFilter{Language: "ENG", Limit: 10}); err == nil {
		t.Fatal("ListClusters() error = nil, want a rejection for the non-canonical \"EN\"")
	}
}

func TestListClustersRejectsBadFilters(t *testing.T) {
	cases := []struct {
		name   string
		filter ClusterFilter
		field  string
	}{
		{"unknown pillar", ClusterFilter{Pillar: "news", Limit: 10}, "pillar"},
		{"unknown language code", ClusterFilter{Language: "fr1", Limit: 10}, "language"},
		{"language in upper case", ClusterFilter{Language: "FR", Limit: 10}, "language"},
		{"zero limit", ClusterFilter{Limit: 0}, "limit"},
		{"limit above max", ClusterFilter{Limit: MaxClusterLimit + 1}, "limit"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newTestService(t, &fakeStore{})

			_, err := service.ListClusters(context.Background(), tc.filter)
			if err == nil {
				t.Fatal("ListClusters() error = nil, want a validation error")
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want *ValidationError", err)
			}
			if validation.Field != tc.field {
				t.Errorf("field = %q, want %q", validation.Field, tc.field)
			}
			if !errors.Is(err, ErrValidation) {
				t.Error("errors.Is(err, ErrValidation) = false, want true")
			}
		})
	}
}

func TestListClustersReturnsEmptySliceNotNil(t *testing.T) {
	service := newTestService(t, &fakeStore{clusters: nil})

	clusters, err := service.ListClusters(context.Background(), ClusterFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if clusters == nil {
		t.Fatal("clusters = nil, want an empty slice")
	}
	if len(clusters) != 0 {
		t.Errorf("clusters = %d, want 0", len(clusters))
	}
}

func TestListClustersWrapsStoreError(t *testing.T) {
	storeErr := errors.New("boom")
	service := newTestService(t, &fakeStore{clustersErr: storeErr})

	_, err := service.ListClusters(context.Background(), ClusterFilter{Limit: 10})
	if !errors.Is(err, storeErr) {
		t.Errorf("error = %v, want it to wrap %v", err, storeErr)
	}
}

func TestListStoriesAtPlaceNormalisesPlaceAndEncodesCursor(t *testing.T) {
	next := stories.NewCursor(testCursorTime, testCursorID)
	store := &fakeStore{
		page: []stories.Story{{ID: testCursorID}},
		next: &next,
	}
	service := newTestService(t, store)

	page, nextCursor, err := service.ListStoriesAtPlace(context.Background(), "  Cape Town  ", "", 10)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}

	if store.gotPlace != "cape town" {
		t.Errorf("store place = %q, want %q (trimmed and lower-cased)", store.gotPlace, "cape town")
	}
	if store.gotCursor != nil {
		t.Errorf("store cursor = %v, want nil for a first page", store.gotCursor)
	}
	if nextCursor != next.Encode() {
		t.Errorf("next cursor = %q, want %q", nextCursor, next.Encode())
	}
	if len(page) != 1 {
		t.Errorf("page = %d, want 1", len(page))
	}
}

func TestListStoriesAtPlaceDecodesCursor(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store)

	raw := stories.NewCursor(testCursorTime, testCursorID).Encode()
	if _, _, err := service.ListStoriesAtPlace(context.Background(), "Cape Town", raw, 10); err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}

	if store.gotCursor == nil {
		t.Fatal("store cursor = nil, want a decoded cursor")
	}
	if store.gotCursor.ID() != testCursorID {
		t.Errorf("cursor id = %q, want %q", store.gotCursor.ID(), testCursorID)
	}
	if !store.gotCursor.CreatedAt().Equal(testCursorTime) {
		t.Errorf("cursor time = %v, want %v", store.gotCursor.CreatedAt(), testCursorTime)
	}
}

func TestListStoriesAtPlaceEmptyLastPage(t *testing.T) {
	service := newTestService(t, &fakeStore{page: nil, next: nil})

	page, next, err := service.ListStoriesAtPlace(context.Background(), "Cape Town", "", 10)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if page == nil {
		t.Error("page = nil, want an empty slice")
	}
	if next != "" {
		t.Errorf("next cursor = %q, want empty on the last page", next)
	}
}

func TestListStoriesAtPlaceRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		place  string
		cursor string
		limit  int
		field  string
	}{
		{"blank place", "   ", "", 10, "place"},
		{"zero limit", "Cape Town", "", 0, "limit"},
		{"limit above max", "Cape Town", "", MaxPlaceLimit + 1, "limit"},
		{"malformed cursor", "Cape Town", "not-a-cursor", 10, "cursor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newTestService(t, &fakeStore{})

			_, _, err := service.ListStoriesAtPlace(context.Background(), tc.place, tc.cursor, tc.limit)
			if err == nil {
				t.Fatal("ListStoriesAtPlace() error = nil, want a validation error")
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want *ValidationError", err)
			}
			if validation.Field != tc.field {
				t.Errorf("field = %q, want %q", validation.Field, tc.field)
			}
		})
	}
}

func TestListStoriesAtPlaceWrapsStoreError(t *testing.T) {
	storeErr := errors.New("boom")
	service := newTestService(t, &fakeStore{listErr: storeErr})

	_, _, err := service.ListStoriesAtPlace(context.Background(), "Cape Town", "", 10)
	if !errors.Is(err, storeErr) {
		t.Errorf("error = %v, want it to wrap %v", err, storeErr)
	}
}
