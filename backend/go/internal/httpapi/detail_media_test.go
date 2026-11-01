package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/versions"
)

// The media array on the story and version detail responses (KNOT-036-fix3).
//
// Every item carries the derivative URLs KNOT-037 generates, built from the object
// keys exactly as the feed's first_media_url is. The assertions here are about the
// WIRE SHAPE: a client must be able to render a thumbnail without knowing a key,
// and must be told to fall back to the original when there is no rendition.

const (
	testDetailMediaStoryID = "c7000000-0000-4000-8000-000000000001"
	testDetailMediaItemID  = "c7000000-0000-4000-8000-000000000002"
	testDetailVersionID    = "c7000000-0000-4000-8000-000000000003"
)

// mediaWithDerivatives is a video with every rendition generated.
func mediaWithDerivatives() storymedia.StoryMedia {
	thumb := storymedia.StoryMediaKeyPrefix(testDetailMediaStoryID) + "thumb.jpg"
	medium := storymedia.StoryMediaKeyPrefix(testDetailMediaStoryID) + "medium.jpg"
	poster := storymedia.StoryMediaKeyPrefix(testDetailMediaStoryID) + "poster.jpg"

	return storymedia.StoryMedia{
		ID:           testDetailMediaItemID,
		StoryID:      testDetailMediaStoryID,
		MediaType:    storymedia.MediaTypeVideo,
		MimeType:     "video/mp4",
		Source:       storymedia.MediaSourceGallery,
		SizeBytes:    2048,
		DisplayOrder: 0,
		CreatedAt:    testNow,
		ThumbnailKey: &thumb,
		MediumKey:    &medium,
		PosterKey:    &poster,
	}
}

// mediaWithoutDerivatives is a legacy row: KNOT-037 added the key columns, and
// everything written before it (or whose renditions failed) has none.
func mediaWithoutDerivatives() storymedia.StoryMedia {
	return storymedia.StoryMedia{
		ID:           testDetailMediaItemID,
		StoryID:      testDetailMediaStoryID,
		MediaType:    storymedia.MediaTypeImage,
		MimeType:     "image/jpeg",
		Source:       storymedia.MediaSourceCamera,
		SizeBytes:    1024,
		DisplayOrder: 0,
		CreatedAt:    testNow,
	}
}

// TestStoryDetailMediaCarriesDerivativeURLs proves the story detail's media array
// addresses the renditions, not only the original.
func TestStoryDetailMediaCarriesDerivativeURLs(t *testing.T) {
	story := storyFixture(testDetailMediaStoryID, authorA)
	lookup := &fakeStoryMediaLookup{
		byStory: map[string][]storymedia.StoryMedia{
			testDetailMediaStoryID: {mediaWithDerivatives()},
		},
	}

	handler := newRouterWithMediaLookup(t, discardLogger(), &fakeStoriesService{getResult: story}, lookup)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testDetailMediaStoryID, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)
	if len(body.Story.Media) != 1 {
		t.Fatalf("media = %d items, want 1", len(body.Story.Media))
	}

	item := body.Story.Media[0]
	original := "/stories/" + testDetailMediaStoryID + "/media/" + testDetailMediaItemID + "/content"
	// The full address is the STREAM: this fixture is a video, and since
	// KNOT-037-fix7 a video's primary object is its stream rendition.
	if want := original + "?variant=stream"; item.URL != want {
		t.Errorf("url = %q, want %q", item.URL, want)
	}

	// A video has no cover any more: it autoplays, so the thumbnail address is
	// null even though this row still carries a poster frame (KNOT-037-fix7).
	if item.ThumbnailURL != nil {
		t.Errorf("thumbnail_url = %q, want null for a video", *item.ThumbnailURL)
	}

	for name, expectation := range map[string]struct {
		got  *string
		want string
	}{
		"medium_url": {item.MediumURL, original + "?variant=medium"},
		"poster_url": {item.PosterURL, original + "?variant=poster"},
	} {
		if expectation.got == nil {
			t.Errorf("%s = null, want a rendition URL", name)
			continue
		}
		if *expectation.got != expectation.want {
			t.Errorf("%s = %q, want %q", name, *expectation.got, expectation.want)
		}
	}

	// A video never advertises a thumbnail fallback.
	if item.ThumbnailURLFallback != nil {
		t.Errorf("thumbnail_url_fallback = %q, want null", *item.ThumbnailURLFallback)
	}
}

// TestStoryThumbnailFallbackIsTheWebPSibling proves a row written by the
// KNOT-037-fix6 pipeline advertises both thumbnail files: the AVIF thumbnail and
// the WebP sibling beside it.
func TestStoryThumbnailFallbackIsTheWebPSibling(t *testing.T) {
	media := mediaWithDerivatives()
	// An image whose thumbnail is the AVIF written by KNOT-037-fix6.
	media.MediaType = storymedia.MediaTypeImage
	media.StorageKey = storymedia.StoryMediaKeyPrefix(testDetailMediaStoryID) + "upload_display.avif"
	thumb := storymedia.StoryMediaKeyPrefix(testDetailMediaStoryID) + "upload_thumb.avif"
	media.ThumbnailKey = &thumb

	lookup := &fakeStoryMediaLookup{
		byStory: map[string][]storymedia.StoryMedia{
			testDetailMediaStoryID: {media},
		},
	}

	handler := newRouterWithMediaLookup(t, discardLogger(),
		&fakeStoriesService{getResult: storyFixture(testDetailMediaStoryID, authorA)}, lookup)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testDetailMediaStoryID, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)
	if len(body.Story.Media) != 1 {
		t.Fatalf("media = %d items, want 1", len(body.Story.Media))
	}

	item := body.Story.Media[0]
	base := "/stories/" + testDetailMediaStoryID + "/media/" + testDetailMediaItemID + "/content"
	if want := base + "?variant=display"; item.URL != want {
		t.Errorf("url = %q, want %q", item.URL, want)
	}
	if item.ThumbnailURL == nil || *item.ThumbnailURL != base+"?variant=thumb" {
		t.Errorf("thumbnail_url = %v, want the thumbnail address", item.ThumbnailURL)
	}
	if item.ThumbnailURLFallback == nil || *item.ThumbnailURLFallback != base+"?variant=thumb&format=webp" {
		t.Errorf("thumbnail_url_fallback = %v, want the webp address", item.ThumbnailURLFallback)
	}
}

// TestStoryDetailMediaWithoutDerivativesFallsBackToTheOriginal pins the contract
// KNOT-037 established: a missing rendition is null, and the client falls back to
// url. It is null rather than a duplicate original URL so a client can tell
// "there is no rendition" from "the rendition is the original".
func TestStoryDetailMediaWithoutDerivativesFallsBackToTheOriginal(t *testing.T) {
	lookup := &fakeStoryMediaLookup{
		byStory: map[string][]storymedia.StoryMedia{
			testDetailMediaStoryID: {mediaWithoutDerivatives()},
		},
	}

	handler := newRouterWithMediaLookup(t, discardLogger(),
		&fakeStoriesService{getResult: storyFixture(testDetailMediaStoryID, authorA)}, lookup)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testDetailMediaStoryID, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)
	if len(body.Story.Media) != 1 {
		t.Fatalf("media = %d items, want 1", len(body.Story.Media))
	}

	item := body.Story.Media[0]
	if item.URL == "" {
		t.Error("url is empty; a legacy item must still be readable through the original")
	}
	if item.ThumbnailURL != nil || item.MediumURL != nil || item.PosterURL != nil {
		t.Errorf("derivatives = (%v, %v, %v), want all null", item.ThumbnailURL, item.MediumURL, item.PosterURL)
	}
}

// versionDetailFixture is a version of the story whose media the tests seed:
// versionFixture defaults to another story id, and a version detail resolves its
// media through its own story.
func versionDetailFixture() versions.StoryVersion {
	version := versionFixture(testDetailVersionID, authorA)
	version.StoryID = testDetailMediaStoryID
	return version
}

// TestVersionDetailCarriesTheStoryMediaDerivatives proves a version detail mirrors
// the story detail's media array, derivatives included (KNOT-036-fix3).
func TestVersionDetailCarriesTheStoryMediaDerivatives(t *testing.T) {
	lookup := &fakeStoryMediaLookup{
		byStory: map[string][]storymedia.StoryMedia{
			testDetailMediaStoryID: {mediaWithDerivatives()},
		},
	}

	handler := newRouterWithFeedLookups(t, discardLogger(), &fakeStoriesService{}, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        lookup,
		reactions:    &fakeReactionsLookup{},
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		versions:     &fakeVersionsService{getResult: versionDetailFixture()},
	})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testDetailVersionID, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body versionDetailEnvelope
	decodeBody(t, recorder, &body)
	if body.Version.ID != testDetailVersionID {
		t.Fatalf("version id = %q, want %q", body.Version.ID, testDetailVersionID)
	}
	if len(body.Version.Media) != 1 {
		t.Fatalf("media = %d items, want the story's 1", len(body.Version.Media))
	}

	item := body.Version.Media[0]
	// A video carries no cover since KNOT-037-fix7, but the poster and medium
	// renditions the row still names keep being advertised.
	if item.ThumbnailURL != nil {
		t.Errorf("thumbnail_url = %q, want null for a video", *item.ThumbnailURL)
	}
	if item.PosterURL == nil || item.MediumURL == nil {
		t.Fatalf("derivatives = (%v, %v), want the poster and the medium",
			item.PosterURL, item.MediumURL)
	}
	base := "/stories/" + testDetailMediaStoryID + "/media/" + testDetailMediaItemID + "/content"
	if want := base + "?variant=poster"; *item.PosterURL != want {
		t.Errorf("poster_url = %q, want %q", *item.PosterURL, want)
	}
}

// TestVersionDetailMediaIsAnEmptyArrayWithoutMedia keeps the response shape stable:
// a story with no media answers `[]`, never null, exactly as the story detail does.
func TestVersionDetailMediaIsAnEmptyArrayWithoutMedia(t *testing.T) {
	handler := newRouterWithFeedLookups(t, discardLogger(), &fakeStoriesService{}, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        &fakeStoryMediaLookup{},
		reactions:    &fakeReactionsLookup{},
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		versions:     &fakeVersionsService{getResult: versionFixture(testDetailVersionID, authorA)},
	})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testDetailVersionID, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if got := recorder.Body.String(); !strings.Contains(got, `"media":[]`) {
		t.Errorf("body = %s, want an empty media array", got)
	}
}

// TestCachedVersionDetailKeepsTheMediaArray guards the cache envelope: the detail
// is stored as a versionDetailEnvelope, so a hit must not silently drop the media
// the database path attached.
func TestCachedVersionDetailKeepsTheMediaArray(t *testing.T) {
	details := newDetailCacheRecorder()
	lookup := &fakeStoryMediaLookup{
		byStory: map[string][]storymedia.StoryMedia{
			testDetailMediaStoryID: {mediaWithDerivatives()},
		},
	}
	versionsService := &fakeVersionsService{getResult: versionDetailFixture()}

	handler := newRouterWithFeedLookups(t, discardLogger(), &fakeStoriesService{}, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        lookup,
		reactions:    &fakeReactionsLookup{},
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		versions:     versionsService,
		caches:       Caches{Details: details},
	})

	for attempt := 1; attempt <= 2; attempt++ {
		recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testDetailVersionID, "", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want %d (body %s)", attempt, recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var body versionDetailEnvelope
		decodeBody(t, recorder, &body)
		if len(body.Version.Media) != 1 || body.Version.Media[0].PosterURL == nil {
			t.Fatalf("attempt %d: media = %+v, want the story's item with its poster", attempt, body.Version.Media)
		}
	}

	// The second read came from the cache, so the service was asked once.
	if versionsService.getCalls != 1 {
		t.Errorf("versions service reads = %d, want 1 (the second read is a cache hit)", versionsService.getCalls)
	}
}
