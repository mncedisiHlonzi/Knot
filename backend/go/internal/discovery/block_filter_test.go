package discovery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/moderation"
)

// blockedSet returns the request context a signed-in viewer carries once the
// given authors are hidden from them: exactly the value AuthMiddleware resolves
// from the viewer's mutual block set (KNOT-ADR-060) and attaches to the context.
// The discovery store reads it with moderation.ExcludedAuthors, so a store-level
// test drives the filter by building the same context (KNOT-ADR-064).
func blockedSet(authorIDs ...string) context.Context {
	return moderation.WithExcludedAuthors(context.Background(), authorIDs)
}

// TestBlockFilterClustersExcludeBlockedAuthor proves that a place whose only
// story is authored by a blocked user disappears from the cluster list entirely,
// because the predicate is applied before the grouping.
func TestBlockFilterClustersExcludeBlockedAuthor(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()

	blocked := createUser(t, pool, prefix, "blocked")
	place := "Hidden Place " + token
	createStoryAt(t, pool, blocked, place, "wonder", "eng", time.Now().Add(-2*time.Hour).UTC())

	clusters, err := store.ListClusters(blockedSet(blocked), ClusterFilter{Limit: DefaultClusterLimit})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if got := ours(clusters, token); len(got) != 0 {
		t.Fatalf("ListClusters() with %q blocked = %+v, want the place to be absent", blocked, got)
	}

	// Without a block set the place is visible, carrying its single story.
	all, err := store.ListClusters(context.Background(), ClusterFilter{Limit: DefaultClusterLimit})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	visible := ours(all, token)
	if len(visible) != 1 {
		t.Fatalf("ListClusters() anonymous = %d clusters for the place, want 1: %+v", len(visible), visible)
	}
	if visible[0].StoryCount != 1 {
		t.Fatalf("anonymous StoryCount = %d, want 1", visible[0].StoryCount)
	}
}

// TestBlockFilterClustersKeepVisibleStoriesAtSharedPlace proves the filtered
// cluster keeps only the visible author's story: the count and the language set
// are computed from the rows that survive the predicate.
func TestBlockFilterClustersKeepVisibleStoriesAtSharedPlace(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()

	blocked := createUser(t, pool, prefix, "blocked")
	visible := createUser(t, pool, prefix, "visible")
	place := "Shared Place " + token
	base := time.Now().Add(-3 * time.Hour).UTC()
	createStoryAt(t, pool, blocked, place, "wonder", "eng", base)
	createStoryAt(t, pool, visible, place, "heritage", "afr", base.Add(time.Minute))

	clusters, err := store.ListClusters(blockedSet(blocked), ClusterFilter{Limit: DefaultClusterLimit})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	got := ours(clusters, token)
	if len(got) != 1 {
		t.Fatalf("ListClusters() = %d clusters for the place, want 1: %+v", len(got), got)
	}
	if got[0].StoryCount != 1 {
		t.Fatalf("StoryCount = %d, want 1 (only the visible author's story)", got[0].StoryCount)
	}
	if len(got[0].Languages) != 1 || got[0].Languages[0] != "afr" {
		t.Fatalf("Languages = %v, want [afr] (the blocked author's language is excluded)", got[0].Languages)
	}
}

// TestBlockFilterPlaceListExcludesBlockedAuthor proves the place story list hides
// a blocked author's story while an anonymous reader still sees it.
func TestBlockFilterPlaceListExcludesBlockedAuthor(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()

	blocked := createUser(t, pool, prefix, "blocked")
	place := "Place List " + token
	createStoryAt(t, pool, blocked, place, "wonder", "eng", time.Now().Add(-time.Hour).UTC())
	placeLower := strings.ToLower(place)

	page, next, err := store.ListStoriesAtPlace(blockedSet(blocked), placeLower, PlaceCountryFilter{}, nil, DefaultPlaceLimit)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if next != nil {
		t.Fatalf("ListStoriesAtPlace() next = %v, want nil for a single page", next)
	}
	if len(page) != 0 {
		t.Fatalf("ListStoriesAtPlace() with %q blocked = %d stories, want 0: %+v", blocked, len(page), page)
	}

	anonymous, _, err := store.ListStoriesAtPlace(context.Background(), placeLower, PlaceCountryFilter{}, nil, DefaultPlaceLimit)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if len(anonymous) != 1 || anonymous[0].AuthorID != blocked {
		t.Fatalf("ListStoriesAtPlace() anonymous = %+v, want the blocked author's single story", anonymous)
	}
}

// TestBlockFilterAnonymousReaderSeesAllStories proves the filter is inert without
// a token: an anonymous reader carries no block set, so both endpoints return
// every story.
func TestBlockFilterAnonymousReaderSeesAllStories(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()

	first := createUser(t, pool, prefix, "first")
	second := createUser(t, pool, prefix, "second")
	place := "Anonymous Place " + token
	base := time.Now().Add(-4 * time.Hour).UTC()
	createStoryAt(t, pool, first, place, "wonder", "eng", base)
	createStoryAt(t, pool, second, place, "heritage", "afr", base.Add(time.Minute))
	placeLower := strings.ToLower(place)

	clusters, err := store.ListClusters(context.Background(), ClusterFilter{Limit: DefaultClusterLimit})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	got := ours(clusters, token)
	if len(got) != 1 {
		t.Fatalf("ListClusters() anonymous = %d clusters for the place, want 1: %+v", len(got), got)
	}
	if got[0].StoryCount != 2 {
		t.Fatalf("anonymous StoryCount = %d, want 2", got[0].StoryCount)
	}

	page, _, err := store.ListStoriesAtPlace(context.Background(), placeLower, PlaceCountryFilter{}, nil, DefaultPlaceLimit)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if len(page) != 2 {
		t.Fatalf("ListStoriesAtPlace() anonymous = %d stories, want 2: %+v", len(page), page)
	}
}

// TestBlockFilterUnblockRestoresDiscoveryStories proves the filter is a pure
// function of the viewer's block set: lifting the block restores the story in
// both endpoints, with no other change.
func TestBlockFilterUnblockRestoresDiscoveryStories(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()

	author := createUser(t, pool, prefix, "author")
	place := "Restored Place " + token
	createStoryAt(t, pool, author, place, "wonder", "eng", time.Now().Add(-time.Hour).UTC())
	placeLower := strings.ToLower(place)

	// While blocked, both endpoints hide the author's only story.
	blockedClusters, err := store.ListClusters(blockedSet(author), ClusterFilter{Limit: DefaultClusterLimit})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	if got := ours(blockedClusters, token); len(got) != 0 {
		t.Fatalf("ListClusters() with %q blocked = %+v, want the place to be absent", author, got)
	}
	blockedPage, _, err := store.ListStoriesAtPlace(blockedSet(author), placeLower, PlaceCountryFilter{}, nil, DefaultPlaceLimit)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if len(blockedPage) != 0 {
		t.Fatalf("ListStoriesAtPlace() with %q blocked = %d stories, want 0", author, len(blockedPage))
	}

	// After the block is lifted the viewer carries an empty set — the same context
	// AuthMiddleware produces from an empty block list — so the story reappears.
	clusters, err := store.ListClusters(context.Background(), ClusterFilter{Limit: DefaultClusterLimit})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}
	got := ours(clusters, token)
	if len(got) != 1 || got[0].StoryCount != 1 {
		t.Fatalf("ListClusters() after unblock = %+v, want one cluster with 1 story", got)
	}

	page, _, err := store.ListStoriesAtPlace(context.Background(), placeLower, PlaceCountryFilter{}, nil, DefaultPlaceLimit)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if len(page) != 1 || page[0].AuthorID != author {
		t.Fatalf("ListStoriesAtPlace() after unblock = %+v, want the author's single story", page)
	}
}
