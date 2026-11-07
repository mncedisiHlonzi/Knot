package httpapi

// The feed's media stills (KNOT-042).
//
// A card used to hold `first_media_url` alone, and for a video that address is
// the multi-megabyte stream: the frame stayed black until the player had decoded
// it. The payload now hydrates the still a card can draw immediately — the
// thumbnail for an image, the extracted poster for a video — on both the feed and
// the detail, and every media item advertises its own `poster_url`.
//
// These tests assert the WIRE shape, because a missing field is invisible to the
// type system and silent in the client: the client simply draws its placeholder.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
)

// Identifiers used only by this file, so a change elsewhere cannot make these
// assertions accidentally true.
const (
	testStillVideoStoryID = "77777777-7777-4777-8777-777777777771"
	testStillImageStoryID = "77777777-7777-4777-8777-777777777772"
	testStillVideoID      = "88888888-8888-4888-8888-888888888881"
	testStillImageID      = "88888888-8888-4888-8888-888888888882"
)

// videoMedia is a ready video: a stream rendition and the poster the worker
// extracted beside it. A video has no thumbnail — it plays (KNOT-042).
func videoMedia() storymedia.StoryMedia {
	posterKey := "story-media/" + testStillVideoStoryID + "/clip_poster.webp"

	return storymedia.StoryMedia{
		ID:         testStillVideoID,
		StoryID:    testStillVideoStoryID,
		MediaType:  storymedia.MediaTypeVideo,
		StorageKey: "story-media/" + testStillVideoStoryID + "/clip_stream.mp4",
		PosterKey:  &posterKey,
		Status:     storymedia.StatusReady,
	}
}

// imageMedia is a ready image: an AVIF display and the WebP thumbnail the feed
// card loads. An image is its own poster, so it has none (KNOT-042).
func imageMedia() storymedia.StoryMedia {
	thumbnailKey := "story-media/" + testStillImageStoryID + "/photo_thumb.webp"

	return storymedia.StoryMedia{
		ID:           testStillImageID,
		StoryID:      testStillImageStoryID,
		MediaType:    storymedia.MediaTypeImage,
		StorageKey:   "story-media/" + testStillImageStoryID + "/photo_display.avif",
		ThumbnailKey: &thumbnailKey,
		Status:       storymedia.StatusReady,
	}
}

// TestFeedPayloadHydratesTheMediaStills proves a feed page carries both stills,
// so a card can draw instantly without a second request.
func TestFeedPayloadHydratesTheMediaStills(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeStoriesService{listResult: feedOf(
		stories.Story{ID: testStillVideoStoryID, AuthorID: testUserID},
		stories.Story{ID: testStillImageStoryID, AuthorID: testUserID},
	)}
	media := &fakeStoryMediaLookup{first: map[string]storymedia.StoryMedia{
		testStillVideoStoryID: videoMedia(),
		testStillImageStoryID: imageMedia(),
	}}

	handler := newRouterWithMediaLookup(t, logger, service, media)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	// The wire names both fields, so a client can rely on their presence even when
	// their value is null. The ~20px placeholder was retired, so neither of its
	// fields is sent any more (KNOT-045).
	for _, want := range []string{
		`"first_media_thumbnail_url"`,
		`"first_media_poster_url"`,
		`"poster_url"`,
	} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("feed body = %s, want it to contain %s", recorder.Body.String(), want)
		}
	}
	for _, gone := range []string{`"first_media_lqip_url"`, `"lqip_url"`} {
		if strings.Contains(recorder.Body.String(), gone) {
			t.Errorf("feed body = %s, want it NOT to contain %s", recorder.Body.String(), gone)
		}
	}

	var body feedPage
	decodeBody(t, recorder, &body)
	page := feedStories(t, body)
	if len(page) != 2 {
		t.Fatalf("stories = %d, want 2", len(page))
	}

	byID := make(map[string]storyResponse, len(page))
	for _, entry := range page {
		byID[entry.ID] = entry
	}

	videoEntry, ok := byID[testStillVideoStoryID]
	if !ok {
		t.Fatalf("the video story is missing from the page")
	}
	if videoEntry.FirstMediaPosterURL == nil ||
		!strings.HasSuffix(*videoEntry.FirstMediaPosterURL, "?variant=poster") {
		t.Errorf("first_media_poster_url = %v, want the poster address", videoEntry.FirstMediaPosterURL)
	}
	if videoEntry.FirstMediaThumbnailURL != nil {
		t.Errorf("first_media_thumbnail_url = %v, want null for a video", *videoEntry.FirstMediaThumbnailURL)
	}
	if len(videoEntry.Media) != 1 {
		t.Fatalf("video media array = %d items, want 1", len(videoEntry.Media))
	}
	if videoEntry.Media[0].PosterURL == nil {
		t.Error("media[0].poster_url = null, want the extracted poster")
	}
	if videoEntry.Media[0].ThumbnailURL != nil {
		t.Errorf("media[0].thumbnail_url = %v, want null for a video", *videoEntry.Media[0].ThumbnailURL)
	}
	// The preview is still the stream: the poster is what a card draws BEFORE it,
	// not a replacement for the address the viewer opens (KNOT-037).
	if videoEntry.FirstMediaURL == nil ||
		!strings.HasSuffix(*videoEntry.FirstMediaURL, "?variant=stream") {
		t.Errorf("first_media_url = %v, want the stream address", videoEntry.FirstMediaURL)
	}

	imageEntry, ok := byID[testStillImageStoryID]
	if !ok {
		t.Fatalf("the image story is missing from the page")
	}
	if imageEntry.FirstMediaThumbnailURL == nil ||
		!strings.HasSuffix(*imageEntry.FirstMediaThumbnailURL, "?variant=thumb") {
		t.Errorf("first_media_thumbnail_url = %v, want the thumbnail address", imageEntry.FirstMediaThumbnailURL)
	}
	if imageEntry.FirstMediaPosterURL != nil {
		t.Errorf("first_media_poster_url = %v, want null for an image", *imageEntry.FirstMediaPosterURL)
	}
	if len(imageEntry.Media) != 1 {
		t.Fatalf("image media array = %d items, want 1", len(imageEntry.Media))
	}
	if imageEntry.Media[0].PosterURL != nil {
		t.Errorf("media[0].poster_url = %v, want null for an image", *imageEntry.Media[0].PosterURL)
	}
	if imageEntry.Media[0].ThumbnailURL == nil {
		t.Error("media[0].thumbnail_url = null, want the image's thumbnail")
	}
}

// TestStoryDetailAdvertisesThePoster proves the detail carries the same still: the
// media viewer opens on `url`, and the poster is the underlay it fades in over.
func TestStoryDetailAdvertisesThePoster(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeStoriesService{getResult: stories.Story{ID: testStillVideoStoryID, AuthorID: testUserID}}
	media := &fakeStoryMediaLookup{byStory: map[string][]storymedia.StoryMedia{
		testStillVideoStoryID: {videoMedia()},
	}}

	handler := newRouterWithMediaLookup(t, logger, service, media)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStillVideoStoryID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if len(body.Story.Media) != 1 {
		t.Fatalf("media = %d items, want 1", len(body.Story.Media))
	}
	poster := body.Story.Media[0].PosterURL
	if poster == nil || !strings.Contains(*poster, testStillVideoID) ||
		!strings.HasSuffix(*poster, "?variant=poster") {
		t.Fatalf("media[0].poster_url = %v, want this item's poster", poster)
	}
	if body.Story.FirstMediaPosterURL == nil || *body.Story.FirstMediaPosterURL != *poster {
		t.Errorf("first_media_poster_url = %v, want %v", body.Story.FirstMediaPosterURL, *poster)
	}
}

// TestMissingPosterStaysNull proves a video whose job produced no still reports
// null rather than an address that would 404: the client must be able to tell "no
// poster" from "a poster".
func TestMissingPosterStaysNull(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	posterless := videoMedia()
	posterless.PosterKey = nil

	service := &fakeStoriesService{getResult: stories.Story{ID: testStillVideoStoryID, AuthorID: testUserID}}
	media := &fakeStoryMediaLookup{byStory: map[string][]storymedia.StoryMedia{
		testStillVideoStoryID: {posterless},
	}}

	handler := newRouterWithMediaLookup(t, logger, service, media)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStillVideoStoryID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if len(body.Story.Media) != 1 {
		t.Fatalf("media = %d items, want 1", len(body.Story.Media))
	}
	if body.Story.Media[0].PosterURL != nil {
		t.Errorf("media[0].poster_url = %v, want null", *body.Story.Media[0].PosterURL)
	}
	if body.Story.FirstMediaPosterURL != nil {
		t.Errorf("first_media_poster_url = %v, want null", *body.Story.FirstMediaPosterURL)
	}
}

// TestStoryMediaPreviewsDoNotSharePosterState guards the batched read: each
// story's preview must be projected from its OWN row, so one item's poster can
// never appear on another's card.
func TestStoryMediaPreviewsDoNotSharePosterState(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	lookup := &fakeStoryMediaLookup{first: map[string]storymedia.StoryMedia{
		testStillVideoStoryID: videoMedia(),
		testStillImageStoryID: imageMedia(),
	}}

	previews := storyMediaPreviews(
		context.Background(),
		lookup,
		logger,
		[]string{testStillVideoStoryID, testStillImageStoryID},
	)

	video := firstMediaFields(previews[testStillVideoStoryID])
	image := firstMediaFields(previews[testStillImageStoryID])

	if video.posterURL == nil || video.thumbnailURL != nil {
		t.Errorf("video stills = poster %v / thumbnail %v, want poster only", video.posterURL, video.thumbnailURL)
	}
	if image.thumbnailURL == nil || image.posterURL != nil {
		t.Errorf("image stills = poster %v / thumbnail %v, want thumbnail only", image.posterURL, image.thumbnailURL)
	}
}
