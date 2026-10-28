package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/storystats"
)

// intPtr returns a pointer to a copy of value, for a fixture whose field is a
// nullable integer on the wire (media dimensions).
func intPtr(value int) *int { return &value }

// fakeStoryStatsLookup is a StoryStatsLookup a test can seed with per-story
// counts and per-version comment counts. Left unseeded it is inert: every story
// reads as the zero Counts, which is what a router assembled for another
// handler's tests wants.
type fakeStoryStatsLookup struct {
	byStory map[string]storystats.Counts
	// byVersion is the single-version comment count, keyed by version id: the
	// read a version detail makes. A version absent from it answers zero.
	byVersion map[string]int
	err       error
	// versionErr fails the single-version read only, so a test can prove the
	// detail degrades to zero without disturbing the page read.
	versionErr error

	gotIDs        []string
	gotVersionIDs []string
}

func (f *fakeStoryStatsLookup) CountsForStories(_ context.Context, storyIDs []string) (map[string]storystats.Counts, error) {
	f.gotIDs = storyIDs
	if f.err != nil {
		return nil, f.err
	}

	out := make(map[string]storystats.Counts, len(storyIDs))
	for _, id := range storyIDs {
		if counts, ok := f.byStory[id]; ok {
			out[id] = counts
		}
	}

	return out, nil
}

func (f *fakeStoryStatsLookup) CommentCountForVersion(_ context.Context, versionID string) (int, error) {
	f.gotVersionIDs = append(f.gotVersionIDs, versionID)
	if f.versionErr != nil {
		return 0, f.versionErr
	}

	// An explicit per-version answer wins; otherwise fall back to the batched map,
	// the way the store answers both reads from the same rows. That keeps a test's
	// detail read consistent with its page read unless it deliberately says
	// otherwise (KNOT-036-fix5).
	if count, ok := f.byVersion[versionID]; ok {
		return count, nil
	}
	for _, counts := range f.byStory {
		if count, ok := counts.CommentCountByVersion[versionID]; ok {
			return count, nil
		}
	}

	return 0, nil
}

// testStatsStoryID is a well-formed story id the stats tests share.
const testStatsStoryID = "22222222-2222-4222-8222-222222222222"

// TestStoryListCarriesAggregateCounts proves GET /stories attaches the version,
// language, comment, and media counts and the first media item's kind, URL, and
// dimensions in one batched read (KNOT-020c-fix, KNOT-020g-fix5).
func TestStoryListCarriesAggregateCounts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeStoriesService{listResult: feedOf(stories.Story{ID: testStatsStoryID, AuthorID: testUserID})}
	media := &fakeStoryMediaLookup{first: map[string]storymedia.StoryMedia{
		testStatsStoryID: {
			ID:        "33333333-3333-4333-8333-333333333333",
			StoryID:   testStatsStoryID,
			MediaType: storymedia.MediaTypeVideo,
			Width:     intPtr(1080),
			Height:    intPtr(1920),
		},
	}}
	// The comment count is the ROOT version's thread: a story entry reports the
	// conversation its own page opens, not the whole tree's (KNOT-036-fix5).
	stats := &fakeStoryStatsLookup{byStory: map[string]storystats.Counts{
		testStatsStoryID: {Versions: 4, Languages: 3, RootComments: 14, Media: 5},
	}}

	handler := newRouterWithLookups(t, logger, service, media, stats)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	// The wire shape names the new fields, so a client can rely on them.
	for _, want := range []string{
		`"version_count":4`,
		`"language_count":3`,
		`"comment_count":14`,
		`"media_count":5`,
		`"first_media_type":"video"`,
		`"first_media_width":1080`,
		`"first_media_height":1920`,
	} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("list body = %s, want it to contain %s", recorder.Body.String(), want)
		}
	}

	var body feedPage
	decodeBody(t, recorder, &body)
	page := feedStories(t, body)
	if len(page) != 1 {
		t.Fatalf("stories = %d, want 1", len(page))
	}

	got := page[0]
	if got.VersionCount != 4 || got.LanguageCount != 3 || got.CommentCount != 14 || got.MediaCount != 5 {
		t.Errorf("counts = %d/%d/%d/%d, want 4/3/14/5", got.VersionCount, got.LanguageCount, got.CommentCount, got.MediaCount)
	}
	if got.FirstMediaType == nil || *got.FirstMediaType != "video" {
		t.Errorf("first_media_type = %v, want video", got.FirstMediaType)
	}
	if got.FirstMediaURL == nil || !strings.HasSuffix(*got.FirstMediaURL, "/content") {
		t.Errorf("first_media_url = %v, want the media content path", got.FirstMediaURL)
	}
	// The dimensions travel with the preview so a feed entry can frame the media
	// at its natural ratio instead of a fixed one (KNOT-020g-fix5).
	if got.FirstMediaWidth == nil || *got.FirstMediaWidth != 1080 {
		t.Errorf("first_media_width = %v, want 1080", got.FirstMediaWidth)
	}
	if got.FirstMediaHeight == nil || *got.FirstMediaHeight != 1920 {
		t.Errorf("first_media_height = %v, want 1920", got.FirstMediaHeight)
	}

	// The counts are one batched read for the whole page, not one per story.
	if len(stats.gotIDs) != 1 || stats.gotIDs[0] != testStatsStoryID {
		t.Errorf("stats lookup ids = %v, want [%s]", stats.gotIDs, testStatsStoryID)
	}
}

// TestStoryDetailCarriesAggregateCounts proves GET /stories/{id} attaches the
// same counts, and that media_count 0 and null first-media fields are still
// present when the story has no media.
func TestStoryDetailCarriesAggregateCounts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeStoriesService{getResult: stories.Story{ID: testStatsStoryID, AuthorID: testUserID}}
	stats := &fakeStoryStatsLookup{byStory: map[string]storystats.Counts{
		testStatsStoryID: {Versions: 2, Languages: 2, RootComments: 3},
	}}

	handler := newRouterWithLookups(t, logger, service, &fakeStoryMediaLookup{}, stats)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStatsStoryID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	for _, want := range []string{`"version_count":2`, `"language_count":2`, `"comment_count":3`, `"media_count":0`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("detail body = %s, want it to contain %s", recorder.Body.String(), want)
		}
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)
	if body.Story.VersionCount != 2 || body.Story.LanguageCount != 2 || body.Story.CommentCount != 3 {
		t.Errorf("counts = %d/%d/%d, want 2/2/3", body.Story.VersionCount, body.Story.LanguageCount, body.Story.CommentCount)
	}
	if body.Story.FirstMediaType != nil || body.Story.FirstMediaURL != nil {
		t.Errorf("first media = %v/%v, want null/null with no media", body.Story.FirstMediaType, body.Story.FirstMediaURL)
	}
	if body.Story.FirstMediaWidth != nil || body.Story.FirstMediaHeight != nil {
		t.Errorf("first media dimensions = %v/%v, want null/null with no media", body.Story.FirstMediaWidth, body.Story.FirstMediaHeight)
	}
}

// TestStoryCountsSwallowLookupFailure proves a stats failure does not fail the
// read: the story is still served, with zero counts.
func TestStoryCountsSwallowLookupFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeStoriesService{getResult: stories.Story{ID: testStatsStoryID, AuthorID: testUserID}}
	stats := &fakeStoryStatsLookup{err: io.ErrUnexpectedEOF}

	handler := newRouterWithLookups(t, logger, service, &fakeStoryMediaLookup{}, stats)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStatsStoryID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (enrichment failure must not fail the read)", recorder.Code, http.StatusOK)
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)
	if body.Story.VersionCount != 0 || body.Story.LanguageCount != 0 || body.Story.CommentCount != 0 || body.Story.MediaCount != 0 {
		t.Errorf("counts = %+v, want all zero after a stats failure", body.Story)
	}
}
