package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/storystats"
	"github.com/knot/backend/internal/versions"
)

// The ids and clock the merged-feed tests work with. They are distinct from every
// other fixture in this package so a failure names the row it is about.
const (
	feedStoryID       = "90000000-0000-4000-8000-000000000001"
	feedRootVersionID = "90000000-0000-4000-8000-000000000002"
	feedVersionID     = "90000000-0000-4000-8000-000000000003"
	feedAdapterID     = "90000000-0000-4000-8000-000000000004"
	feedAuthorID      = "90000000-0000-4000-8000-000000000005"
	feedMediaID       = "90000000-0000-4000-8000-000000000006"
)

const (
	feedAdapterName = "Sipho Dlamini"
	feedAuthorName  = "Nomsa Khan"
	// feedAttribution is the exact sentence the server renders for the fixture
	// below: a two-link chain, an original in English adapted into Zulu
	// (KNOT-021, KNOT-022).
	feedAttribution = "Adapted from Nomsa Khan's original story in English into Zulu."
)

// feedFixture builds the store, the enrichment dependencies, and the router for
// the merged-feed tests: one story and one adaptation of it, the adaptation written
// a minute later by somebody else.
//
// It returns the assembled dependencies as well, so a test can assert what each
// decoration cost.
func feedFixture(t *testing.T) (http.Handler, *memoryStoryStore, feedLookups) {
	t.Helper()

	story := stories.Story{
		ID:                  feedStoryID,
		AuthorID:            feedAuthorID,
		RootVersionID:       feedRootVersionID,
		Pillar:              stories.PillarHeritage,
		Language:            "eng",
		Title:               "The first rain",
		Body:                "Grandmother said the first rain remembers every name.",
		ApproximateLocation: "Manguzi",
		PlaceCountry:        optionalString("South Africa"),
		MediaURLs:           []string{},
		CreatedAt:           testNow.Add(-time.Minute),
		UpdatedAt:           testNow.Add(-time.Minute),
	}

	version := stories.VersionFeedItem{
		ID:                  feedVersionID,
		StoryID:             feedStoryID,
		ParentVersionID:     feedRootVersionID,
		AuthorID:            feedAdapterID,
		Language:            "zul",
		Title:               "Imvula yokuqala",
		Body:                "Ugogo wathi imvula yokuqala ikhumbula wonke amagama.",
		Pillar:              stories.PillarHeritage,
		ApproximateLocation: "Manguzi",
		MediaURLs:           []string{},
		CreatedAt:           testNow,
		UpdatedAt:           testNow,
	}

	store := &memoryStoryStore{stories: []stories.Story{story}, versions: []stories.VersionFeedItem{version}}

	lookups := feedLookups{
		authors: &fakeAuthorService{users: map[string]*identity.User{
			feedAuthorID:  {ID: feedAuthorID, DisplayName: feedAuthorName},
			feedAdapterID: {ID: feedAdapterID, DisplayName: feedAdapterName},
		}},
		rooted: &fakeRootedService{batchResult: rootedSignals(map[string]string{feedAdapterID: "Durban"})},
		media: &fakeStoryMediaLookup{first: map[string]storymedia.StoryMedia{
			feedStoryID: sampleMedia(feedStoryID, feedMediaID),
		}},
		reactions: &fakeReactionsLookup{summaries: map[string]reactions.Summary{
			feedStoryID:   {RingsTrue: 5},
			feedVersionID: {AddsSomethingNew: 2},
		}},
		stats: &fakeStoryStatsLookup{byStory: map[string]storystats.Counts{
			feedStoryID: {Versions: 2, Languages: 2, Comments: 3, Media: 1},
		}},
		chains: &fakeVersionChainLookup{chains: map[string]versions.Chain{
			feedVersionID: {
				{
					VersionID:         feedRootVersionID,
					StoryID:           feedStoryID,
					AuthorID:          feedAuthorID,
					AuthorDisplayName: feedAuthorName,
					Language:          "eng",
					Depth:             0,
				},
				{
					VersionID:         feedVersionID,
					StoryID:           feedStoryID,
					AuthorID:          feedAdapterID,
					AuthorDisplayName: feedAdapterName,
					Language:          "zul",
					Depth:             1,
				},
			},
		}},
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}

	handler := newRouterWithFeedLookups(t, slog.New(slog.NewTextHandler(io.Discard, nil)), service, lookups)

	return handler, store, lookups
}

// TestFeedCarriesAdaptationEntries is the central handler test of KNOT-022: one
// page carries the story AND the adaptation somebody wrote of it, newest first,
// each with the identity the client needs to open the right thing.
func TestFeedCarriesAdaptationEntries(t *testing.T) {
	handler, _, _ := feedFixture(t)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2 (the story and its adaptation)", len(body.Items))
	}

	// The adaptation was written a minute later, so it leads -- it sorts by its own
	// created_at, not by its story's.
	if kinds := feedKinds(t, body); kinds[0] != feedKindVersion || kinds[1] != feedKindStory {
		t.Fatalf("kinds = %v, want [%s %s]", kinds, feedKindVersion, feedKindStory)
	}

	adaptations := feedVersions(t, body)
	if len(adaptations) != 1 {
		t.Fatalf("adaptations = %d, want 1", len(adaptations))
	}

	adaptation := adaptations[0]
	if adaptation.ID != feedVersionID {
		t.Errorf("id = %q, want the VERSION %q", adaptation.ID, feedVersionID)
	}
	if adaptation.StoryID != feedStoryID {
		t.Errorf("story id = %q, want the story it adapts %q", adaptation.StoryID, feedStoryID)
	}
	if adaptation.ParentVersionID != feedRootVersionID {
		t.Errorf("parent version id = %q, want %q", adaptation.ParentVersionID, feedRootVersionID)
	}
	if adaptation.AuthorID != feedAdapterID {
		t.Errorf("author id = %q, want the adapter %q", adaptation.AuthorID, feedAdapterID)
	}
	if adaptation.AuthorDisplayName != feedAdapterName {
		t.Errorf("author display name = %q, want %q", adaptation.AuthorDisplayName, feedAdapterName)
	}
	if adaptation.AuthorRooted == nil || adaptation.AuthorRooted.Place != "Durban" {
		t.Errorf("author rooted = %+v, want the adapter's inline summary", adaptation.AuthorRooted)
	}
	if adaptation.Language != "zul" || adaptation.Title != "Imvula yokuqala" {
		t.Errorf("content = %q/%q, want the adaptation's own language and title", adaptation.Language, adaptation.Title)
	}
	if adaptation.Pillar != stories.PillarHeritage {
		t.Errorf("pillar = %q, want the story's pillar inherited", adaptation.Pillar)
	}
	if adaptation.ApproximateLocation != "Manguzi" {
		t.Errorf("location = %q, want the story's place inherited", adaptation.ApproximateLocation)
	}
	if adaptation.VersionNumber != 2 {
		t.Errorf("version number = %d, want 2 (the original is 1)", adaptation.VersionNumber)
	}
	if adaptation.AttributionLine != feedAttribution {
		t.Errorf("attribution line = %q, want %q", adaptation.AttributionLine, feedAttribution)
	}
	if !adaptation.CreatedAt.Equal(testNow) {
		t.Errorf("created at = %v, want the version's own timestamp %v", adaptation.CreatedAt, testNow)
	}
	if adaptation.PreviewTruncated {
		t.Error("preview truncated = true, want false for a body inside the preview window")
	}

	// The story's media and counts describe the story, so an adaptation entry shows
	// the story behind it.
	if adaptation.MediaCount != 1 {
		t.Errorf("media count = %d, want the story's 1", adaptation.MediaCount)
	}
	if adaptation.FirstMediaType == nil || *adaptation.FirstMediaType != "image" {
		t.Errorf("first media type = %v, want image", adaptation.FirstMediaType)
	}
	if adaptation.FirstMediaURL == nil || !strings.Contains(*adaptation.FirstMediaURL, feedMediaID) {
		t.Errorf("first media url = %v, want the story's media", adaptation.FirstMediaURL)
	}
	if adaptation.VersionCount != 2 || adaptation.LanguageCount != 2 || adaptation.CommentCount != 3 {
		t.Errorf(
			"counts = %d/%d/%d, want the story's 2/2/3",
			adaptation.VersionCount, adaptation.LanguageCount, adaptation.CommentCount,
		)
	}

	// Reactions are the adaptation's own: a reader reacts to the version they are
	// reading, not to the story behind it.
	if adaptation.Reactions.AddsSomethingNew != 2 {
		t.Errorf("reaction counts = %+v, want the version's adds_something_new 2", adaptation.Reactions)
	}
	if adaptation.Reactions.RingsTrue != 0 {
		t.Errorf("rings_true = %d, want 0: the version is not the story", adaptation.Reactions.RingsTrue)
	}
	if adaptation.MyReactions == nil {
		t.Error("my_reactions = null, want an empty array for a signed-out reader")
	}

	// The story entry is unchanged, and it has no version identity of its own.
	storiesInPage := feedStories(t, body)
	if len(storiesInPage) != 1 {
		t.Fatalf("stories = %d, want 1", len(storiesInPage))
	}
	storyEntry := storiesInPage[0]
	if storyEntry.ID != feedStoryID {
		t.Errorf("story id = %q, want %q", storyEntry.ID, feedStoryID)
	}
	if storyEntry.AuthorDisplayName != feedAuthorName {
		t.Errorf("story author = %q, want the story's own author %q", storyEntry.AuthorDisplayName, feedAuthorName)
	}
	if storyEntry.Reactions.RingsTrue != 5 {
		t.Errorf("story reactions = %+v, want the story's own counts", storyEntry.Reactions)
	}
	if !storyEntry.CreatedAt.Equal(testNow.Add(-time.Minute)) {
		t.Errorf("story created at = %v, want its own timestamp", storyEntry.CreatedAt)
	}

	// A story entry must not pretend to be a version: version_number and
	// attribution_line belong to the adaptation's shape.
	for _, raw := range body.Items {
		if feedKindOf(t, raw) != feedKindStory {
			continue
		}
		if strings.Contains(string(raw), "version_number") {
			t.Errorf("story entry = %s, want no version_number", raw)
		}
		if strings.Contains(string(raw), "attribution_line") {
			t.Errorf("story entry = %s, want no attribution_line", raw)
		}
	}
}

// TestFeedAttributesAPageOfAdaptationsInOneRead pins the batching contract: the
// lineage of every adaptation on the page comes from one read, and the people the
// lines name are resolved in the same author read as the adapters themselves, so
// attribution never becomes a query per entry or per link (KNOT-021, KNOT-022).
func TestFeedAttributesAPageOfAdaptationsInOneRead(t *testing.T) {
	handler, _, lookups := feedFixture(t)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	chains := lookups.chains.(*fakeVersionChainLookup)
	if chains.calls != 1 {
		t.Errorf("chain reads = %d, want exactly 1 for the whole page", chains.calls)
	}
	if len(chains.gotIDs) != 1 || chains.gotIDs[0] != feedVersionID {
		t.Errorf("chain ids = %v, want just the adaptation on the page", chains.gotIDs)
	}

	// One author read serves the whole page -- both kinds and the lineage's links
	// alike -- so a mixed page costs one identity read, not one per kind.
	authors := lookups.authors.(*fakeAuthorService)
	if authors.calls != 1 {
		t.Errorf("author reads = %d, want exactly 1 for the whole page", authors.calls)
	}
	if !containsString(authors.gotIDs, feedAdapterID) || !containsString(authors.gotIDs, feedAuthorID) {
		t.Errorf("author ids = %v, want the adapter and the original author named in the same read", authors.gotIDs)
	}
}

// TestFeedSkipsAttributionWhenTheStoreHasNoVersions pins that the chain read is
// skipped entirely when a page carries no adaptations, so a stories-only page
// costs nothing extra.
func TestFeedSkipsAttributionWhenThePageHasNoAdaptations(t *testing.T) {
	handler, _, lookups := feedFixture(t)

	// Ask for a page that ends before the adaptation: limit 1 returns the newest
	// entry, which here is the adaptation, so this asks for the story's own page
	// instead by starting after the adaptation.
	recorder := doStoryRequest(handler, http.MethodGet, "/stories?limit=1", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var first feedPage
	decodeBody(t, recorder, &first)
	if len(first.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(first.Items))
	}

	// The first page's single entry is the adaptation, so the chain lookup ran
	// once. Resuming from its cursor returns only stories.
	chains := lookups.chains.(*fakeVersionChainLookup)
	if chains.calls != 1 {
		t.Fatalf("chain reads = %d, want 1 for the page that carried the adaptation", chains.calls)
	}

	recorder = doStoryRequest(handler, http.MethodGet, "/stories?limit=5&cursor="+first.NextCursor, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var second feedPage
	decodeBody(t, recorder, &second)
	if kinds := feedKinds(t, second); len(kinds) != 1 || kinds[0] != feedKindStory {
		t.Fatalf("second page kinds = %v, want just the story", kinds)
	}
	if chains.calls != 1 {
		t.Errorf("chain reads = %d, want the stories-only page not to read lineages", chains.calls)
	}
}

// TestFeedKeepsAdaptationsReadableWhenAttributionFails pins that attribution is
// enrichment: a failed lineage read leaves the entry in place without a sentence
// rather than failing the page, and the failure is logged rather than returned.
func TestFeedKeepsAdaptationsReadableWhenAttributionFails(t *testing.T) {
	handler, _, lookups := feedFixture(t)
	lookups.chains.(*fakeVersionChainLookup).err = errors.New("versions table is unavailable")

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	adaptations := feedVersions(t, body)
	if len(adaptations) != 1 {
		t.Fatalf("adaptations = %d, want the entry to stay readable", len(adaptations))
	}
	if adaptations[0].AttributionLine != "" {
		t.Errorf("attribution line = %q, want an empty line when the lineage is unavailable", adaptations[0].AttributionLine)
	}
	if adaptations[0].VersionNumber != 0 {
		t.Errorf("version number = %d, want 0 rather than a guess", adaptations[0].VersionNumber)
	}

	// The entry is still fully identified, which is the point: a reader can open it.
	if adaptations[0].ID != feedVersionID || adaptations[0].StoryID != feedStoryID {
		t.Errorf("entry identity = %q/%q, want the version and its story", adaptations[0].ID, adaptations[0].StoryID)
	}
}

// TestFeedReadsAdaptationAndStoryReactionsSeparately pins that the two kinds key
// their reactions by different entities: an adaptation's counts come from the
// version, a story's from the story, so a page must read each type.
func TestFeedReadsAdaptationAndStoryReactionsSeparately(t *testing.T) {
	handler, _, lookups := feedFixture(t)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	lookup := lookups.reactions.(*fakeReactionsLookup)
	if lookup.summaryCalls != 2 {
		t.Errorf("reaction reads = %d, want 2 (one per entity type)", lookup.summaryCalls)
	}
	// The story group is enriched first, so the last read the fake saw is the
	// adaptation group's.
	if lookup.gotEntityType != reactions.EntityVersion {
		t.Errorf("last entity type = %q, want %q", lookup.gotEntityType, reactions.EntityVersion)
	}

	// Both id spaces were read: the story id and the version id.
	if !containsString(lookup.gotEntityIDs, feedStoryID) || !containsString(lookup.gotEntityIDs, feedVersionID) {
		t.Errorf("reaction ids = %v, want both the story and the version", lookup.gotEntityIDs)
	}
}

// TestFeedAdaptationBodyIsAlwaysPresent pins the shape a card depends on: the
// whole body travels even when it is longer than the preview window, and the flag
// says whether the card should offer "read more" (KNOT-020g-fix12).
func TestFeedAdaptationBodyIsAlwaysPresent(t *testing.T) {
	handler, store, _ := feedFixture(t)
	store.versions[0].Body = strings.Repeat("b", previewWindowChars+1)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	adaptations := feedVersions(t, body)
	if len(adaptations) != 1 {
		t.Fatalf("adaptations = %d, want 1", len(adaptations))
	}
	if len(adaptations[0].Body) != previewWindowChars+1 {
		t.Errorf("body length = %d, want the whole body", len(adaptations[0].Body))
	}
	if !adaptations[0].PreviewTruncated {
		t.Error("preview truncated = false, want true for a body past the window")
	}
	if !strings.Contains(recorder.Body.String(), `"kind":"version"`) {
		t.Errorf("body = %s, want the discriminator on the wire", recorder.Body.String())
	}
}

// TestFeedDecodesCursorFromTheMergedStream pins that the cursor a page returns
// resumes exactly where the page ended, whichever kind the last entry was.
func TestFeedDecodesCursorFromTheMergedStream(t *testing.T) {
	handler, store, _ := feedFixture(t)

	// The adaptation is the newest entry, so the first page's cursor points at it.
	recorder := doStoryRequest(handler, http.MethodGet, "/stories?limit=1", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var first feedPage
	decodeBody(t, recorder, &first)
	if first.NextCursor == "" {
		t.Fatal("next cursor is empty, want a cursor pointing at the adaptation")
	}

	cursor, err := stories.DecodeCursor(first.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}
	if !cursor.CreatedAt().Equal(testNow) || cursor.ID() != feedVersionID {
		t.Errorf("cursor = %v/%s, want the version's own key %v/%s", cursor.CreatedAt(), cursor.ID(), testNow, feedVersionID)
	}
	if store.listCalls != 1 {
		t.Errorf("list calls = %d, want 1", store.listCalls)
	}
}

// TestFeedEntryKindIsAlwaysPresent pins the contract a client switches on: every
// entry carries a kind, and it is one of the two the API documents.
func TestFeedEntryKindIsAlwaysPresent(t *testing.T) {
	handler, _, _ := feedFixture(t)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	for i, kind := range feedKinds(t, body) {
		if kind != feedKindStory && kind != feedKindVersion {
			t.Errorf("item %d kind = %q, want %q or %q", i, kind, feedKindStory, feedKindVersion)
		}
	}

	// The raw body is checked too: an unknown kind must be visible in the JSON, not
	// merely inferable from the decoded structs.
	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw body: %v", err)
	}
	for i, item := range raw.Items {
		if _, ok := item["kind"]; !ok {
			t.Errorf("item %d has no kind key", i)
		}
	}
}

// containsString reports whether values holds want.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
