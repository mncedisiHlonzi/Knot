package stories

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// authorID is canonical UUID text used as the authenticated author in tests.
const authorID = "11111111-1111-4111-8111-111111111111"

// fakeStore records the calls it received and returns canned results, so the
// business rules can be tested without a database.
type fakeStore struct {
	createResult Story
	createErr    error
	getResult    Story
	getErr       error
	listResult   []Story
	listNext     *Cursor
	listErr      error

	gotCreate Story
	gotGetID  string
	gotCursor *Cursor
	gotLimit  int

	createCalls int
	getCalls    int
	listCalls   int
}

func (f *fakeStore) CreateStory(_ context.Context, story Story) (Story, error) {
	f.createCalls++
	f.gotCreate = story
	if f.createErr != nil {
		return Story{}, f.createErr
	}

	result := f.createResult
	if result.ID == "" {
		// Emulate the database generating identity and timestamps.
		result = story
		result.ID = "22222222-2222-4222-8222-222222222222"
		result.CreatedAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
		result.UpdatedAt = result.CreatedAt
	}

	return result, nil
}

func (f *fakeStore) GetStory(_ context.Context, id string) (Story, error) {
	f.getCalls++
	f.gotGetID = id
	return f.getResult, f.getErr
}

func (f *fakeStore) ListStories(_ context.Context, cursor *Cursor, limit int) ([]Story, *Cursor, error) {
	f.listCalls++
	f.gotCursor = cursor
	f.gotLimit = limit
	return f.listResult, f.listNext, f.listErr
}

// newTestService returns a service over a fresh fake store.
func newTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()

	store := &fakeStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, store
}

// validInput is a minimal input that passes every rule.
func validInput() CreateStoryInput {
	return CreateStoryInput{
		AuthorID: authorID,
		Pillar:   PillarWonder,
		Language: "eng",
		Title:    "The first rain",
		Body:     "Grandmother said the first rain remembers every name.",
	}
}

func TestNewServiceRejectsMissingStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Error("NewService(nil) error = nil, want an error")
	}
}

func TestCreateStoryHappyPath(t *testing.T) {
	service, store := newTestService(t)

	created, err := service.CreateStory(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	if created.ID == "" {
		t.Error("id is empty, want the stored id")
	}
	if store.gotCreate.AuthorID != authorID {
		t.Errorf("author id = %q, want %q", store.gotCreate.AuthorID, authorID)
	}
	if created.MediaURLs == nil {
		t.Error("media urls = nil, want an empty slice so the column never receives NULL")
	}
	if created.Sensitive {
		t.Error("sensitive = true, want false by default")
	}
}

func TestCreateStoryTrimsTitleButNotBody(t *testing.T) {
	service, store := newTestService(t)

	input := validInput()
	input.Title = "  The first rain  "
	input.Body = "  indented body  "

	if _, err := service.CreateStory(context.Background(), input); err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	if store.gotCreate.Title != "The first rain" {
		t.Errorf("title = %q, want the trimmed value", store.gotCreate.Title)
	}
	if store.gotCreate.Body != "  indented body  " {
		t.Errorf("body = %q, want the author's formatting preserved", store.gotCreate.Body)
	}
}

func TestCreateStoryTrimsLanguageButRequiresACanonicalCode(t *testing.T) {
	service, store := newTestService(t)

	// Surrounding whitespace is trimmed...
	input := validInput()
	input.Language = " eng "

	if _, err := service.CreateStory(context.Background(), input); err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	if store.gotCreate.Language != "eng" {
		t.Errorf("language = %q, want %q", store.gotCreate.Language, "eng")
	}

	// ...but the code is not case-folded: a story's language is a canonical
	// ISO 639-3 code, so "ENG" is a mistake rather than something to guess at.
	input.Language = "ENG"

	if _, err := service.CreateStory(context.Background(), input); err == nil {
		t.Fatal("CreateStory() error = nil, want a rejection for the non-canonical \"ENG\"")
	}
}

// floatPtr returns a pointer to v, for the optional coordinate fields.
func floatPtr(v float64) *float64 { return &v }

func TestCreateStoryStoresCoordinates(t *testing.T) {
	service, store := newTestService(t)

	input := validInput()
	input.ApproximateLocation = "Manguzi"
	input.Latitude = floatPtr(-26.9998)
	input.Longitude = floatPtr(32.7489)
	input.PlaceCountry = " South Africa "

	created, err := service.CreateStory(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	if store.gotCreate.Latitude == nil || *store.gotCreate.Latitude != -26.9998 {
		t.Errorf("latitude = %v, want -26.9998", store.gotCreate.Latitude)
	}
	if store.gotCreate.Longitude == nil || *store.gotCreate.Longitude != 32.7489 {
		t.Errorf("longitude = %v, want 32.7489", store.gotCreate.Longitude)
	}
	if created.PlaceCountry == nil || *created.PlaceCountry != "South Africa" {
		t.Errorf("place country = %v, want the trimmed value", created.PlaceCountry)
	}
}

func TestCreateStoryWithoutCoordinatesLeavesThemNil(t *testing.T) {
	service, store := newTestService(t)

	if _, err := service.CreateStory(context.Background(), validInput()); err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}
	if store.gotCreate.Latitude != nil || store.gotCreate.Longitude != nil {
		t.Errorf("coordinates = (%v, %v), want nil", store.gotCreate.Latitude, store.gotCreate.Longitude)
	}
	if store.gotCreate.PlaceCountry != nil {
		t.Errorf("place country = %v, want nil", store.gotCreate.PlaceCountry)
	}
}

func TestCreateStoryAcceptsBoundaryCoordinates(t *testing.T) {
	for _, test := range []struct{ lat, lng float64 }{{-90, -180}, {90, 180}} {
		service, _ := newTestService(t)

		input := validInput()
		input.Latitude = floatPtr(test.lat)
		input.Longitude = floatPtr(test.lng)

		if _, err := service.CreateStory(context.Background(), input); err != nil {
			t.Errorf("CreateStory(lat=%v, lng=%v) error = %v, want nil", test.lat, test.lng, err)
		}
	}
}

func TestCreateStoryValidation(t *testing.T) {
	longTitle := strings.Repeat("t", MaxTitleLen+1)
	longBody := strings.Repeat("b", MaxBodyLen+1)
	longLocation := strings.Repeat("l", MaxLocationLength+1)

	tests := []struct {
		name      string
		mutate    func(*CreateStoryInput)
		wantField string
	}{
		{"missing author", func(in *CreateStoryInput) { in.AuthorID = "" }, "author_id"},
		{"author is not a uuid", func(in *CreateStoryInput) { in.AuthorID = "not-a-uuid" }, "author_id"},
		{"missing pillar", func(in *CreateStoryInput) { in.Pillar = "" }, "pillar"},
		{"unknown pillar", func(in *CreateStoryInput) { in.Pillar = "chaos" }, "pillar"},
		{"empty title", func(in *CreateStoryInput) { in.Title = "   " }, "title"},
		{"title too long", func(in *CreateStoryInput) { in.Title = longTitle }, "title"},
		{"empty body", func(in *CreateStoryInput) { in.Body = "\n\t " }, "body"},
		{"body too long", func(in *CreateStoryInput) { in.Body = longBody }, "body"},
		{"unknown language code", func(in *CreateStoryInput) { in.Language = "e" }, "language"},
		{"two letter code", func(in *CreateStoryInput) { in.Language = "en" }, "language"},
		{"language given as a word", func(in *CreateStoryInput) { in.Language = "english" }, "language"},
		{"language in upper case", func(in *CreateStoryInput) { in.Language = "ENG" }, "language"},
		{"language with digits", func(in *CreateStoryInput) { in.Language = "en2" }, "language"},
		{"language with a subtag", func(in *CreateStoryInput) { in.Language = "en-ZA" }, "language"},
		{"location too long", func(in *CreateStoryInput) { in.ApproximateLocation = longLocation }, "approximate_location"},
		{"empty media entry", func(in *CreateStoryInput) { in.MediaURLs = []string{"  "} }, "media_urls"},
		{
			"too many media entries",
			func(in *CreateStoryInput) { in.MediaURLs = make([]string, MaxMediaURLs+1) },
			"media_urls",
		},
		{
			"latitude without longitude",
			func(in *CreateStoryInput) { in.Latitude = floatPtr(1.5) },
			"latitude",
		},
		{
			"longitude without latitude",
			func(in *CreateStoryInput) { in.Longitude = floatPtr(1.5) },
			"latitude",
		},
		{
			"latitude below range",
			func(in *CreateStoryInput) { in.Latitude = floatPtr(-90.1); in.Longitude = floatPtr(0) },
			"latitude",
		},
		{
			"latitude above range",
			func(in *CreateStoryInput) { in.Latitude = floatPtr(90.1); in.Longitude = floatPtr(0) },
			"latitude",
		},
		{
			"longitude below range",
			func(in *CreateStoryInput) { in.Latitude = floatPtr(0); in.Longitude = floatPtr(-180.1) },
			"longitude",
		},
		{
			"longitude above range",
			func(in *CreateStoryInput) { in.Latitude = floatPtr(0); in.Longitude = floatPtr(180.1) },
			"longitude",
		},
		{
			"country too long",
			func(in *CreateStoryInput) { in.PlaceCountry = strings.Repeat("c", MaxPlaceCountryLength+1) },
			"place_country",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store := newTestService(t)

			input := validInput()
			test.mutate(&input)

			if _, err := service.CreateStory(context.Background(), input); err == nil {
				t.Fatal("CreateStory() error = nil, want a validation error")
			} else {
				if !errors.Is(err, ErrValidation) {
					t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
				}

				var validation *ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
				}
				if validation.Field != test.wantField {
					t.Errorf("field = %q, want %q", validation.Field, test.wantField)
				}
			}

			if store.createCalls != 0 {
				t.Errorf("store received %d create calls, want 0 — invalid input must not reach the store", store.createCalls)
			}
		})
	}
}

func TestCreateStoryAcceptsBoundaryLengths(t *testing.T) {
	service, _ := newTestService(t)

	input := validInput()
	input.Title = strings.Repeat("t", MaxTitleLen)
	input.Body = strings.Repeat("b", MaxBodyLen)
	input.Language = "eng"
	input.ApproximateLocation = strings.Repeat("l", MaxLocationLength)
	input.MediaURLs = []string{"https://example.test/a.jpg"}

	if _, err := service.CreateStory(context.Background(), input); err != nil {
		t.Fatalf("CreateStory() error = %v, want nil at the documented limits", err)
	}
}

func TestCreateStoryCountsRunesNotBytes(t *testing.T) {
	service, _ := newTestService(t)

	// Each of these characters is multi-byte, so a byte-based length check would
	// reject input the specification allows.
	input := validInput()
	input.Title = strings.Repeat("é", MaxTitleLen)
	input.Body = strings.Repeat("日", MaxBodyLen)

	if _, err := service.CreateStory(context.Background(), input); err != nil {
		t.Fatalf("CreateStory() error = %v, want nil for multi-byte text within the rune limits", err)
	}
}

func TestCreateStoryStoreFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.createErr = errors.New("connection reset")

	_, err := service.CreateStory(context.Background(), validInput())
	if err == nil {
		t.Fatal("CreateStory() error = nil, want the store error")
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than a domain one", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want the underlying cause preserved", err)
	}
}

func TestGetStoryReturnsStoreResult(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = Story{ID: authorID, Title: "The first rain"}

	story, err := service.GetStory(context.Background(), authorID)
	if err != nil {
		t.Fatalf("GetStory() error = %v, want nil", err)
	}

	if story.Title != "The first rain" {
		t.Errorf("title = %q, want %q", story.Title, "The first rain")
	}
	if store.gotGetID != authorID {
		t.Errorf("store received id %q, want %q", store.gotGetID, authorID)
	}
}

func TestGetStoryMalformedIdIsNotFoundWithoutQueryingTheStore(t *testing.T) {
	service, store := newTestService(t)

	_, err := service.GetStory(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if store.getCalls != 0 {
		t.Errorf("store received %d get calls, want 0 — a malformed id cannot match a row", store.getCalls)
	}
}

func TestGetStoryNotFound(t *testing.T) {
	service, store := newTestService(t)
	store.getErr = ErrNotFound

	_, err := service.GetStory(context.Background(), authorID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestGetStoryStoreFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.getErr = errors.New("connection reset")

	_, err := service.GetStory(context.Background(), authorID)
	if err == nil {
		t.Fatal("GetStory() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}

func TestListStoriesFirstPageHasNoCursor(t *testing.T) {
	service, store := newTestService(t)
	store.listResult = []Story{{ID: authorID}}

	page, next, err := service.ListStories(context.Background(), "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListStories() error = %v, want nil", err)
	}

	if len(page) != 1 {
		t.Errorf("len(page) = %d, want 1", len(page))
	}
	if next != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", next)
	}
	if store.gotCursor != nil {
		t.Errorf("store received cursor %+v, want nil for the first page", store.gotCursor)
	}
}

func TestListStoriesDecodesCursorAndEncodesNext(t *testing.T) {
	service, store := newTestService(t)
	resume := NewCursor(time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), authorID)
	next := NewCursor(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), authorID)
	store.listResult = []Story{{ID: authorID}}
	store.listNext = &next

	_, encoded, err := service.ListStories(context.Background(), resume.Encode(), 5)
	if err != nil {
		t.Fatalf("ListStories() error = %v, want nil", err)
	}

	if store.gotCursor == nil {
		t.Fatal("store received a nil cursor, want the decoded cursor")
	}
	if !store.gotCursor.CreatedAt().Equal(resume.CreatedAt()) || store.gotCursor.ID() != resume.ID() {
		t.Errorf("store cursor = %+v, want %+v", store.gotCursor, resume)
	}
	if encoded != next.Encode() {
		t.Errorf("next cursor = %q, want %q", encoded, next.Encode())
	}
}

func TestListStoriesEmptyPageIsAnEmptySliceNotNil(t *testing.T) {
	service, store := newTestService(t)
	store.listResult = nil

	page, next, err := service.ListStories(context.Background(), "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListStories() error = %v, want nil", err)
	}

	if page == nil {
		t.Error("page = nil, want an empty slice so the response contains [] rather than null")
	}
	if len(page) != 0 {
		t.Errorf("len(page) = %d, want 0", len(page))
	}
	if next != "" {
		t.Errorf("next cursor = %q, want an empty string", next)
	}
}

func TestListStoriesRejectsBadLimitWithoutQueryingTheStore(t *testing.T) {
	tests := []struct {
		name  string
		limit int
	}{
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
		{name: "above the maximum", limit: MaxListLimit + 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store := newTestService(t)

			_, _, err := service.ListStories(context.Background(), "", test.limit)
			if !errors.Is(err, ErrValidation) {
				t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
			}
			if validation.Field != "limit" {
				t.Errorf("field = %q, want %q", validation.Field, "limit")
			}
			if store.listCalls != 0 {
				t.Errorf("store received %d list calls, want 0", store.listCalls)
			}
		})
	}
}

func TestListStoriesRejectsMalformedCursorWithoutQueryingTheStore(t *testing.T) {
	service, store := newTestService(t)

	_, _, err := service.ListStories(context.Background(), "not-a-cursor", DefaultListLimit)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}
	if store.listCalls != 0 {
		t.Errorf("store received %d list calls, want 0 — an unreadable cursor must not reach the database", store.listCalls)
	}
}

func TestListStoriesLimitIsPassedThroughUnchanged(t *testing.T) {
	service, store := newTestService(t)

	if _, _, err := service.ListStories(context.Background(), "", MaxListLimit); err != nil {
		t.Fatalf("ListStories() error = %v, want nil", err)
	}

	if store.gotLimit != MaxListLimit {
		t.Errorf("store limit = %d, want %d", store.gotLimit, MaxListLimit)
	}
}

func TestListStoriesStoreFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.listErr = errors.New("connection reset")

	page, next, err := service.ListStories(context.Background(), "", DefaultListLimit)
	if err == nil {
		t.Fatal("ListStories() error = nil, want the store error")
	}
	if page != nil || next != "" {
		t.Errorf("page = %v, next = %q, want zero values alongside the error", page, next)
	}
}

func TestPillarValid(t *testing.T) {
	tests := []struct {
		pillar Pillar
		want   bool
	}{
		{PillarWonder, true},
		{PillarHeritage, true},
		{Pillar(""), false},
		{Pillar("Wonder"), false},
		{Pillar("chaos"), false},
	}

	for _, test := range tests {
		if got := test.pillar.Valid(); got != test.want {
			t.Errorf("Pillar(%q).Valid() = %v, want %v", test.pillar, got, test.want)
		}
	}
}

func TestValidationErrorImplementsTheSentinelContract(t *testing.T) {
	err := error(&ValidationError{Field: "title", Message: "is required"})

	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) = false, want true")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("Error() = %q, want it to name the field", err.Error())
	}
}
