package versions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Canonical UUID text used throughout the service tests.
const (
	serviceStoryID      = "33333333-3333-4333-8333-333333333333"
	serviceOtherStoryID = "33333333-3333-4333-8333-333333333334"
	serviceParentID     = "44444444-4444-4444-8444-444444444444"
	serviceAuthorID     = "11111111-1111-4111-8111-111111111111"
)

// fakeStore records the calls it received and returns canned results, so the
// business rules can be tested without a database.
type fakeStore struct {
	getResult    StoryVersion
	getErr       error
	createResult StoryVersion
	createErr    error
	listResult   []StoryVersion
	listErr      error

	gotCreate      StoryVersion
	gotGetID       string
	gotListStoryID string

	getCalls    int
	createCalls int
	listCalls   int
}

func (f *fakeStore) CreateVersion(_ context.Context, version StoryVersion) (StoryVersion, error) {
	f.createCalls++
	f.gotCreate = version
	if f.createErr != nil {
		return StoryVersion{}, f.createErr
	}

	result := f.createResult
	if result.ID == "" {
		// Emulate the database generating identity and timestamps.
		result = version
		result.ID = "66666666-6666-4666-8666-666666666666"
		result.CreatedAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
		result.UpdatedAt = result.CreatedAt
	}

	return result, nil
}

func (f *fakeStore) GetVersion(_ context.Context, id string) (StoryVersion, error) {
	f.getCalls++
	f.gotGetID = id
	return f.getResult, f.getErr
}

func (f *fakeStore) ListByStory(_ context.Context, storyID string) ([]StoryVersion, error) {
	f.listCalls++
	f.gotListStoryID = storyID
	return f.listResult, f.listErr
}

// notificationCall is one notification the service asked its notifier to send.
type notificationCall struct {
	recipientID string
	actorID     string
	versionID   string
}

// fakeNotifier records the notifications the service asked it to send and can be
// made to fail, so the Tell My People hook can be asserted without the
// notifications package.
type fakeNotifier struct {
	calls []notificationCall
	err   error
}

func (f *fakeNotifier) NotifyVersionCreated(_ context.Context, recipientID, actorID, versionID string) error {
	f.calls = append(f.calls, notificationCall{recipientID: recipientID, actorID: actorID, versionID: versionID})
	return f.err
}

// newTestService returns a service over a fresh fake store and notifier.
func newTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()

	service, store, _ := newTestServiceWithNotifier(t)
	return service, store
}

// newTestServiceWithNotifier returns a service over a fresh fake store and
// notifier, so a test can inspect the notifications the service sends.
func newTestServiceWithNotifier(t *testing.T) (*Service, *fakeStore, *fakeNotifier) {
	t.Helper()

	store := &fakeStore{}
	notifier := &fakeNotifier{}
	service, err := NewService(store, notifier)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, store, notifier
}

// validAdaptationInput is a minimal input that passes every rule, adapting the
// root version of serviceStoryID.
func validAdaptationInput() CreateAdaptationInput {
	return CreateAdaptationInput{
		StoryID:         serviceStoryID,
		ParentVersionID: serviceParentID,
		AuthorID:        serviceAuthorID,
		Language:        "fr",
		Title:           "La première pluie",
		Body:            "Grand-mère disait que la première pluie se souvient de chaque nom.",
		AdaptationNote:  "Rendered for French-speaking listeners.",
	}
}

// rootParent is the parent lookup result for an input whose parent belongs to
// serviceStoryID.
func rootParent() StoryVersion {
	return StoryVersion{
		ID:       serviceParentID,
		StoryID:  serviceStoryID,
		AuthorID: serviceAuthorID,
		Language: "en",
		Title:    "The first rain",
		Body:     "Grandmother said the first rain remembers every name.",
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeNotifier{}); err == nil {
		t.Error("NewService(nil store) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, nil); err == nil {
		t.Error("NewService(nil notifier) error = nil, want an error")
	}
}

func TestCreateAdaptationHappyPath(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = rootParent()

	created, err := service.CreateAdaptation(context.Background(), validAdaptationInput())
	if err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil", err)
	}

	if created.ID == "" {
		t.Error("id is empty, want the stored id")
	}
	if store.gotGetID != serviceParentID {
		t.Errorf("parent lookup id = %q, want %q", store.gotGetID, serviceParentID)
	}
	if store.gotCreate.StoryID != serviceStoryID {
		t.Errorf("stored story id = %q, want %q", store.gotCreate.StoryID, serviceStoryID)
	}
	if store.gotCreate.ParentVersionID != serviceParentID {
		t.Errorf("stored parent id = %q, want %q", store.gotCreate.ParentVersionID, serviceParentID)
	}
	if store.gotCreate.AuthorID != serviceAuthorID {
		t.Errorf("stored author id = %q, want %q", store.gotCreate.AuthorID, serviceAuthorID)
	}
}

func TestCreateAdaptationNormalisesFields(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = rootParent()

	input := validAdaptationInput()
	input.Language = " FR "
	input.Title = "  La première pluie  "
	input.Body = "  indented body  "
	input.AdaptationNote = "  a note  "

	if _, err := service.CreateAdaptation(context.Background(), input); err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil", err)
	}

	if store.gotCreate.Language != "fr" {
		t.Errorf("language = %q, want %q", store.gotCreate.Language, "fr")
	}
	if store.gotCreate.Title != "La première pluie" {
		t.Errorf("title = %q, want the trimmed value", store.gotCreate.Title)
	}
	if store.gotCreate.Body != "  indented body  " {
		t.Errorf("body = %q, want the author's formatting preserved", store.gotCreate.Body)
	}
	if store.gotCreate.AdaptationNote != "a note" {
		t.Errorf("adaptation note = %q, want the trimmed value", store.gotCreate.AdaptationNote)
	}
}

func TestCreateAdaptationValidation(t *testing.T) {
	longTitle := strings.Repeat("t", MaxTitleLen+1)
	longBody := strings.Repeat("b", MaxBodyLen+1)
	longNote := strings.Repeat("n", MaxAdaptationNoteLength+1)

	tests := []struct {
		name      string
		mutate    func(*CreateAdaptationInput)
		wantField string
	}{
		{"missing parent", func(in *CreateAdaptationInput) { in.ParentVersionID = "" }, "parent_version_id"},
		{"parent is not a uuid", func(in *CreateAdaptationInput) { in.ParentVersionID = "not-a-uuid" }, "parent_version_id"},
		{"missing author", func(in *CreateAdaptationInput) { in.AuthorID = "" }, "author_id"},
		{"author is not a uuid", func(in *CreateAdaptationInput) { in.AuthorID = "not-a-uuid" }, "author_id"},
		{"empty title", func(in *CreateAdaptationInput) { in.Title = "   " }, "title"},
		{"title too long", func(in *CreateAdaptationInput) { in.Title = longTitle }, "title"},
		{"empty body", func(in *CreateAdaptationInput) { in.Body = "\n\t " }, "body"},
		{"body too long", func(in *CreateAdaptationInput) { in.Body = longBody }, "body"},
		{"language too short", func(in *CreateAdaptationInput) { in.Language = "f" }, "language"},
		{"language too long", func(in *CreateAdaptationInput) { in.Language = "englishish" }, "language"},
		{"language with digits", func(in *CreateAdaptationInput) { in.Language = "fr2" }, "language"},
		{"adaptation note too long", func(in *CreateAdaptationInput) { in.AdaptationNote = longNote }, "adaptation_note"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store := newTestService(t)
			store.getResult = rootParent()

			input := validAdaptationInput()
			test.mutate(&input)

			_, err := service.CreateAdaptation(context.Background(), input)
			if err == nil {
				t.Fatal("CreateAdaptation() error = nil, want a validation error")
			}
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
			if store.createCalls != 0 {
				t.Errorf("store received %d create calls, want 0 — invalid input must not reach the store", store.createCalls)
			}
			if store.getCalls != 0 {
				t.Errorf("store received %d get calls, want 0 — validation must run before the parent lookup", store.getCalls)
			}
		})
	}
}

func TestCreateAdaptationMalformedStoryIdIsNotFound(t *testing.T) {
	service, store := newTestService(t)

	input := validAdaptationInput()
	input.StoryID = "not-a-uuid"

	_, err := service.CreateAdaptation(context.Background(), input)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if store.getCalls != 0 || store.createCalls != 0 {
		t.Error("the store was called, want a malformed story id to stop before any query")
	}
}

func TestCreateAdaptationParentNotFound(t *testing.T) {
	service, store := newTestService(t)
	store.getErr = ErrNotFound

	_, err := service.CreateAdaptation(context.Background(), validAdaptationInput())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if store.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a missing parent must not be stored", store.createCalls)
	}
}

func TestCreateAdaptationParentFromAnotherStoryIsRejected(t *testing.T) {
	service, store := newTestService(t)
	parent := rootParent()
	parent.StoryID = serviceOtherStoryID
	store.getResult = parent

	_, err := service.CreateAdaptation(context.Background(), validAdaptationInput())
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
	}
	if validation.Field != "parent_version_id" {
		t.Errorf("field = %q, want %q", validation.Field, "parent_version_id")
	}
	if store.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a cross-story parent must not be stored", store.createCalls)
	}
}

func TestCreateAdaptationAcceptsBoundaryLengths(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = rootParent()

	input := validAdaptationInput()
	input.Title = strings.Repeat("t", MaxTitleLen)
	input.Body = strings.Repeat("b", MaxBodyLen)
	input.AdaptationNote = strings.Repeat("n", MaxAdaptationNoteLength)
	input.Language = "english"

	if _, err := service.CreateAdaptation(context.Background(), input); err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil at the documented limits", err)
	}
}

func TestCreateAdaptationCountsRunesNotBytes(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = rootParent()

	// Each of these characters is multi-byte, so a byte-based length check would
	// reject input the specification allows.
	input := validAdaptationInput()
	input.Title = strings.Repeat("é", MaxTitleLen)
	input.Body = strings.Repeat("日", MaxBodyLen)

	if _, err := service.CreateAdaptation(context.Background(), input); err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil for multi-byte text within the rune limits", err)
	}
}

func TestCreateAdaptationStoreFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = rootParent()
	store.createErr = errors.New("connection reset")

	_, err := service.CreateAdaptation(context.Background(), validAdaptationInput())
	if err == nil {
		t.Fatal("CreateAdaptation() error = nil, want the store error")
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than a domain one", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want the underlying cause preserved", err)
	}
}

func TestCreateAdaptationParentLookupFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.getErr = errors.New("connection reset")

	_, err := service.CreateAdaptation(context.Background(), validAdaptationInput())
	if err == nil {
		t.Fatal("CreateAdaptation() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want the underlying cause preserved", err)
	}
}

func TestGetVersionReturnsStoreResult(t *testing.T) {
	service, store := newTestService(t)
	store.getResult = rootParent()

	version, err := service.GetVersion(context.Background(), serviceParentID)
	if err != nil {
		t.Fatalf("GetVersion() error = %v, want nil", err)
	}

	if version.ID != serviceParentID {
		t.Errorf("id = %q, want %q", version.ID, serviceParentID)
	}
	if store.gotGetID != serviceParentID {
		t.Errorf("store received id %q, want %q", store.gotGetID, serviceParentID)
	}
}

func TestGetVersionMalformedIdIsNotFoundWithoutQueryingTheStore(t *testing.T) {
	service, store := newTestService(t)

	_, err := service.GetVersion(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if store.getCalls != 0 {
		t.Errorf("store received %d get calls, want 0 — a malformed id cannot match a row", store.getCalls)
	}
}

func TestGetVersionStoreFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.getErr = errors.New("connection reset")

	_, err := service.GetVersion(context.Background(), serviceParentID)
	if err == nil {
		t.Fatal("GetVersion() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}

func TestGetTreeReturnsStoreResult(t *testing.T) {
	service, store := newTestService(t)
	store.listResult = []StoryVersion{rootParent()}

	versions, err := service.GetTree(context.Background(), serviceStoryID)
	if err != nil {
		t.Fatalf("GetTree() error = %v, want nil", err)
	}

	if len(versions) != 1 {
		t.Errorf("len(versions) = %d, want 1", len(versions))
	}
	if store.gotListStoryID != serviceStoryID {
		t.Errorf("store received story id %q, want %q", store.gotListStoryID, serviceStoryID)
	}
}

func TestGetTreeMalformedStoryIdIsNotFoundWithoutQueryingTheStore(t *testing.T) {
	service, store := newTestService(t)

	_, err := service.GetTree(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if store.listCalls != 0 {
		t.Errorf("store received %d list calls, want 0 — a malformed id cannot name a story", store.listCalls)
	}
}

func TestGetTreeWithNoVersionsIsAnEmptySliceNotNil(t *testing.T) {
	// Unreachable against a migrated database, where every story has a root
	// version, but the service must still behave: an empty list is [] not null.
	service, store := newTestService(t)
	store.listResult = nil

	versions, err := service.GetTree(context.Background(), serviceStoryID)
	if err != nil {
		t.Fatalf("GetTree() error = %v, want nil", err)
	}
	if versions == nil {
		t.Error("versions = nil, want an empty slice so the response contains [] rather than null")
	}
	if len(versions) != 0 {
		t.Errorf("len(versions) = %d, want 0", len(versions))
	}
}

func TestGetTreeStoryNotFound(t *testing.T) {
	service, store := newTestService(t)
	store.listErr = ErrNotFound

	_, err := service.GetTree(context.Background(), serviceStoryID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestGetTreeStoreFailureIsWrapped(t *testing.T) {
	service, store := newTestService(t)
	store.listErr = errors.New("connection reset")

	_, err := service.GetTree(context.Background(), serviceStoryID)
	if err == nil {
		t.Fatal("GetTree() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}
