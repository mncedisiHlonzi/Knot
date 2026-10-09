package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Canonical UUID text used throughout the service tests.
const (
	authorID        = "11111111-1111-4111-8111-111111111111"
	versionID       = "44444444-4444-4444-8444-444444444444"
	otherVersionID  = "44444444-4444-4444-8444-444444444445"
	sourceCommentID = "66666666-6666-4666-8666-666666666666"
	targetCommentID = "88888888-8888-4888-8888-888888888888"
	bridgeID        = "77777777-7777-4777-8777-777777777777"
	storyID         = "33333333-3333-4333-8333-333333333333"
)

// fakeCommentStore records the calls it received and returns canned results.
type fakeCommentStore struct {
	createResult Comment
	createErr    error
	getResult    Comment
	getErr       error
	listResult   []Comment
	listNext     *Cursor
	listErr      error

	repliesResult []Comment
	repliesErr    error

	versionAuthorResult string
	versionAuthorErr    error

	gotCreate         Comment
	gotGetID          string
	gotVersionID      string
	gotCursor         *Cursor
	gotLimit          int
	gotReplyParentIDs []string

	createCalls        int
	getCalls           int
	listCalls          int
	replyCalls         int
	versionAuthorCalls int
}

func (f *fakeCommentStore) CreateComment(_ context.Context, comment Comment) (Comment, error) {
	f.createCalls++
	f.gotCreate = comment
	if f.createErr != nil {
		return Comment{}, f.createErr
	}

	result := f.createResult
	if result.ID == "" {
		result = comment
		result.ID = targetCommentID
		result.CreatedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		result.UpdatedAt = result.CreatedAt
	}

	return result, nil
}

func (f *fakeCommentStore) GetComment(_ context.Context, id string) (Comment, error) {
	f.getCalls++
	f.gotGetID = id
	return f.getResult, f.getErr
}

func (f *fakeCommentStore) ListComments(_ context.Context, versionID string, cursor *Cursor, limit int) ([]Comment, *Cursor, error) {
	f.listCalls++
	f.gotVersionID = versionID
	f.gotCursor = cursor
	f.gotLimit = limit
	return f.listResult, f.listNext, f.listErr
}

func (f *fakeCommentStore) ListReplies(_ context.Context, parentIDs []string) ([]Comment, error) {
	f.replyCalls++
	f.gotReplyParentIDs = parentIDs
	return f.repliesResult, f.repliesErr
}

func (f *fakeCommentStore) VersionAuthor(_ context.Context, versionID string) (string, error) {
	f.versionAuthorCalls++
	f.gotVersionID = versionID
	return f.versionAuthorResult, f.versionAuthorErr
}

// notificationCall is one notification the service asked its notifier to send.
type notificationCall struct {
	kind        string
	recipientID string
	actorID     string
	entityID    string
}

// fakeNotifier records the notifications the service asked it to send and can be
// made to fail, so the Tell My People hooks can be asserted without the
// notifications package.
type fakeNotifier struct {
	calls []notificationCall
	err   error
}

func (f *fakeNotifier) NotifyCommentCreated(_ context.Context, recipientID, actorID, commentID string) error {
	f.calls = append(f.calls, notificationCall{
		kind:        "comment.created",
		recipientID: recipientID,
		actorID:     actorID,
		entityID:    commentID,
	})
	return f.err
}

func (f *fakeNotifier) NotifyBridgeCreated(_ context.Context, recipientID, actorID, bridgeID string) error {
	f.calls = append(f.calls, notificationCall{
		kind:        "bridge.created",
		recipientID: recipientID,
		actorID:     actorID,
		entityID:    bridgeID,
	})
	return f.err
}

// fakeBridgeStore records the calls it received and returns canned results.
type fakeBridgeStore struct {
	findTargetResult string
	findTargetErr    error
	createResult     Bridge
	createTarget     Comment
	createErr        error
	getResult        Bridge
	getErr           error

	listForCommentResult []Bridge
	listForCommentErr    error
	listForStoryResult   []Bridge
	listForStoryErr      error

	gotTarget          Comment
	gotSourceCommentID string
	gotNote            string
	gotCommentID       string
	gotStoryID         string
	gotSourceVersionID string
	gotTargetLanguage  string

	createCalls         int
	getCalls            int
	listForCommentCalls int
	listForStoryCalls   int
	findTargetCalls     int
}

func (f *fakeBridgeStore) FindTargetVersion(_ context.Context, sourceVersionID, targetLanguage string) (string, error) {
	f.findTargetCalls++
	f.gotSourceVersionID = sourceVersionID
	f.gotTargetLanguage = targetLanguage
	return f.findTargetResult, f.findTargetErr
}

func (f *fakeBridgeStore) CreateBridge(_ context.Context, target Comment, sourceCommentID, adaptationNote string) (Bridge, Comment, error) {
	f.createCalls++
	f.gotTarget = target
	f.gotSourceCommentID = sourceCommentID
	f.gotNote = adaptationNote
	if f.createErr != nil {
		return Bridge{}, Comment{}, f.createErr
	}

	bridge := f.createResult
	if bridge.ID == "" {
		bridge = Bridge{
			ID:              bridgeID,
			SourceCommentID: sourceCommentID,
			TargetCommentID: targetCommentID,
			AuthorID:        target.AuthorID,
			TargetLanguage:  target.Language,
			AdaptationNote:  adaptationNote,
			CreatedAt:       time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		}
	}

	storedTarget := f.createTarget
	if storedTarget.ID == "" {
		storedTarget = target
		storedTarget.ID = targetCommentID
		storedTarget.CreatedAt = bridge.CreatedAt
		storedTarget.UpdatedAt = bridge.CreatedAt
	}

	return bridge, storedTarget, nil
}

func (f *fakeBridgeStore) GetBridge(_ context.Context, id string) (Bridge, error) {
	f.getCalls++
	return f.getResult, f.getErr
}

func (f *fakeBridgeStore) ListBridgesForComment(_ context.Context, commentID string) ([]Bridge, error) {
	f.listForCommentCalls++
	f.gotCommentID = commentID
	return f.listForCommentResult, f.listForCommentErr
}

func (f *fakeBridgeStore) ListBridgesForStory(_ context.Context, storyID string) ([]Bridge, error) {
	f.listForStoryCalls++
	f.gotStoryID = storyID
	return f.listForStoryResult, f.listForStoryErr
}

func newTestService(t *testing.T) (*Service, *fakeCommentStore, *fakeBridgeStore) {
	t.Helper()

	service, comments, bridges, _ := newTestServiceWithNotifier(t)
	return service, comments, bridges
}

// newTestServiceWithNotifier returns a service over fresh fakes and the notifier
// it was wired with, so a test can inspect the notifications the service sends.
func newTestServiceWithNotifier(t *testing.T) (*Service, *fakeCommentStore, *fakeBridgeStore, *fakeNotifier) {
	t.Helper()

	comments := &fakeCommentStore{}
	bridges := &fakeBridgeStore{}
	notifier := &fakeNotifier{}

	service, err := NewService(comments, bridges, notifier)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, comments, bridges, notifier
}

// sourceComment is an English comment on versionID, the typical bridge source.
func sourceComment() Comment {
	return Comment{
		ID:        sourceCommentID,
		VersionID: versionID,
		AuthorID:  authorID,
		Language:  "eng",
		Body:      "The first rain remembers every name.",
	}
}

func validCommentInput() CreateCommentInput {
	return CreateCommentInput{
		VersionID: versionID,
		AuthorID:  authorID,
		Language:  "eng",
		Body:      "The first rain remembers every name.",
	}
}

func validBridgeInput() CreateBridgeInput {
	return CreateBridgeInput{
		SourceCommentID: sourceCommentID,
		AuthorID:        authorID,
		TargetLanguage:  "fra",
		Body:            "La première pluie se souvient de chaque nom.",
		AdaptationNote:  "Rendered for French-speaking listeners.",
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeBridgeStore{}, &fakeNotifier{}); err == nil {
		t.Error("NewService(nil comments) error = nil, want an error")
	}
	if _, err := NewService(&fakeCommentStore{}, nil, &fakeNotifier{}); err == nil {
		t.Error("NewService(nil bridges) error = nil, want an error")
	}
	if _, err := NewService(&fakeCommentStore{}, &fakeBridgeStore{}, nil); err == nil {
		t.Error("NewService(nil notifier) error = nil, want an error")
	}
}

func TestCreateCommentHappyPath(t *testing.T) {
	service, comments, _ := newTestService(t)

	created, err := service.CreateComment(context.Background(), validCommentInput())
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if created.ID == "" {
		t.Error("id is empty, want the stored id")
	}
	if comments.gotCreate.VersionID != versionID {
		t.Errorf("stored version id = %q, want %q", comments.gotCreate.VersionID, versionID)
	}
	if comments.gotCreate.AuthorID != authorID {
		t.Errorf("stored author id = %q, want %q", comments.gotCreate.AuthorID, authorID)
	}
}

func TestCreateCommentTrimsLanguageAndPreservesBody(t *testing.T) {
	service, comments, _ := newTestService(t)

	input := validCommentInput()
	input.Language = " eng "
	input.Body = "  indented body  "

	if _, err := service.CreateComment(context.Background(), input); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if comments.gotCreate.Language != "eng" {
		t.Errorf("language = %q, want %q", comments.gotCreate.Language, "eng")
	}
	if comments.gotCreate.Body != "  indented body  " {
		t.Errorf("body = %q, want the author's formatting preserved", comments.gotCreate.Body)
	}
}

func TestCreateCommentValidation(t *testing.T) {
	longBody := strings.Repeat("b", MaxBodyLen+1)

	tests := []struct {
		name      string
		mutate    func(*CreateCommentInput)
		wantField string
	}{
		{"missing author", func(in *CreateCommentInput) { in.AuthorID = "" }, "author_id"},
		{"author is not a uuid", func(in *CreateCommentInput) { in.AuthorID = "not-a-uuid" }, "author_id"},
		{"empty body", func(in *CreateCommentInput) { in.Body = "   " }, "body"},
		{"body too long", func(in *CreateCommentInput) { in.Body = longBody }, "body"},
		{"unknown language code", func(in *CreateCommentInput) { in.Language = "e" }, "language"},
		{"two letter code", func(in *CreateCommentInput) { in.Language = "en" }, "language"},
		{"language given as a word", func(in *CreateCommentInput) { in.Language = "english" }, "language"},
		{"language in upper case", func(in *CreateCommentInput) { in.Language = "ENG" }, "language"},
		{"language with digits", func(in *CreateCommentInput) { in.Language = "en2" }, "language"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, comments, _ := newTestService(t)

			input := validCommentInput()
			test.mutate(&input)

			_, err := service.CreateComment(context.Background(), input)
			if err == nil {
				t.Fatal("CreateComment() error = nil, want a validation error")
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
			if comments.createCalls != 0 {
				t.Errorf("store received %d create calls, want 0 — invalid input must not reach the store", comments.createCalls)
			}
		})
	}
}

func TestCreateCommentMalformedVersionIdIsNotFound(t *testing.T) {
	service, comments, _ := newTestService(t)

	input := validCommentInput()
	input.VersionID = "not-a-uuid"

	_, err := service.CreateComment(context.Background(), input)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if comments.createCalls != 0 {
		t.Error("the store was called, want a malformed version id to stop before any query")
	}
}

func TestCreateCommentVersionNotFound(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.createErr = ErrNotFound

	_, err := service.CreateComment(context.Background(), validCommentInput())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestCreateCommentStoreFailureIsWrapped(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.createErr = errors.New("connection reset")

	_, err := service.CreateComment(context.Background(), validCommentInput())
	if err == nil {
		t.Fatal("CreateComment() error = nil, want the store error")
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than a domain one", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want the underlying cause preserved", err)
	}
}

func TestListCommentsFirstPageHasNoCursor(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.listResult = []Comment{sourceComment()}

	page, next, err := service.ListComments(context.Background(), versionID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
	}

	if len(page) != 1 {
		t.Errorf("len(page) = %d, want 1", len(page))
	}
	if next != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", next)
	}
	if comments.gotCursor != nil {
		t.Errorf("store received cursor %+v, want nil for the first page", comments.gotCursor)
	}
}

func TestListCommentsDecodesCursorAndEncodesNext(t *testing.T) {
	service, comments, _ := newTestService(t)
	resume := NewCursor(cursorTime, sourceCommentID)
	next := NewCursor(cursorTime.Add(-time.Hour), sourceCommentID)
	comments.listResult = []Comment{sourceComment()}
	comments.listNext = &next

	_, encoded, err := service.ListComments(context.Background(), versionID, resume.Encode(), 5)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
	}

	if comments.gotCursor == nil {
		t.Fatal("store received a nil cursor, want the decoded cursor")
	}
	if !comments.gotCursor.CreatedAt().Equal(resume.CreatedAt()) || comments.gotCursor.ID() != resume.ID() {
		t.Errorf("store cursor = %+v, want %+v", comments.gotCursor, resume)
	}
	if encoded != next.Encode() {
		t.Errorf("next cursor = %q, want %q", encoded, next.Encode())
	}
}

func TestListCommentsEmptyPageIsAnEmptySliceNotNil(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.listResult = nil

	page, next, err := service.ListComments(context.Background(), versionID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
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

func TestListCommentsRejectsBadLimitWithoutQueryingTheStore(t *testing.T) {
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
			service, comments, _ := newTestService(t)

			_, _, err := service.ListComments(context.Background(), versionID, "", test.limit)
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
			if comments.listCalls != 0 {
				t.Errorf("store received %d list calls, want 0", comments.listCalls)
			}
		})
	}
}

func TestListCommentsMalformedVersionIdIsNotFoundWithoutQueryingTheStore(t *testing.T) {
	service, comments, _ := newTestService(t)

	_, _, err := service.ListComments(context.Background(), "not-a-uuid", "", DefaultListLimit)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if comments.listCalls != 0 {
		t.Errorf("store received %d list calls, want 0 — a malformed id cannot name a version", comments.listCalls)
	}
}

func TestListCommentsRejectsMalformedCursorWithoutQueryingTheStore(t *testing.T) {
	service, comments, _ := newTestService(t)

	_, _, err := service.ListComments(context.Background(), versionID, "not-a-cursor", DefaultListLimit)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}
	if comments.listCalls != 0 {
		t.Errorf("store received %d list calls, want 0 — an unreadable cursor must not reach the database", comments.listCalls)
	}
}

func TestListCommentsVersionNotFound(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.listErr = ErrNotFound

	_, _, err := service.ListComments(context.Background(), versionID, "", DefaultListLimit)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestListCommentsStoreFailureIsWrapped(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.listErr = errors.New("connection reset")

	_, _, err := service.ListComments(context.Background(), versionID, "", DefaultListLimit)
	if err == nil {
		t.Fatal("ListComments() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}

func TestCreateBridgeHappyPath(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID

	bridge, source, target, err := service.CreateBridge(context.Background(), validBridgeInput())
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	if bridge.ID == "" {
		t.Error("bridge id is empty, want the stored id")
	}
	if source.ID != sourceCommentID {
		t.Errorf("source id = %q, want %q", source.ID, sourceCommentID)
	}
	if target.ID == "" {
		t.Error("target id is empty, want the stored id")
	}
	if comments.gotGetID != sourceCommentID {
		t.Errorf("source lookup id = %q, want %q", comments.gotGetID, sourceCommentID)
	}
	if bridges.gotSourceCommentID != sourceCommentID {
		t.Errorf("store source id = %q, want %q", bridges.gotSourceCommentID, sourceCommentID)
	}
	// The target version is resolved from the source version and the target
	// language, so the target comment joins the target-language conversation.
	if bridges.gotSourceVersionID != versionID {
		t.Errorf("target lookup source version = %q, want %q", bridges.gotSourceVersionID, versionID)
	}
	if bridges.gotTargetLanguage != "fra" {
		t.Errorf("target lookup language = %q, want %q", bridges.gotTargetLanguage, "fra")
	}
	if bridges.gotTarget.VersionID != otherVersionID {
		t.Errorf("target version id = %q, want the resolved target version %q", bridges.gotTarget.VersionID, otherVersionID)
	}
	if bridges.gotTarget.Language != "fra" {
		t.Errorf("target language = %q, want %q", bridges.gotTarget.Language, "fra")
	}
	if bridges.gotTarget.AuthorID != authorID {
		t.Errorf("target author id = %q, want the bridger %q", bridges.gotTarget.AuthorID, authorID)
	}
	if bridges.gotNote != "Rendered for French-speaking listeners." {
		t.Errorf("note = %q, want the submitted note", bridges.gotNote)
	}
}

func TestCreateBridgeNormalisesFields(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID

	input := validBridgeInput()
	input.TargetLanguage = " fra "
	input.Body = "  indented  "
	input.AdaptationNote = "  a note  "

	if _, _, _, err := service.CreateBridge(context.Background(), input); err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	if bridges.gotTarget.Language != "fra" {
		t.Errorf("target language = %q, want %q", bridges.gotTarget.Language, "fra")
	}
	if bridges.gotTargetLanguage != "fra" {
		t.Errorf("target lookup language = %q, want the normalised %q", bridges.gotTargetLanguage, "fra")
	}
	if bridges.gotTarget.Body != "  indented  " {
		t.Errorf("target body = %q, want the author's formatting preserved", bridges.gotTarget.Body)
	}
	if bridges.gotNote != "a note" {
		t.Errorf("note = %q, want the trimmed value", bridges.gotNote)
	}
}

func TestCreateBridgeValidation(t *testing.T) {
	longBody := strings.Repeat("b", MaxBodyLen+1)
	longNote := strings.Repeat("n", MaxAdaptationNoteLength+1)

	tests := []struct {
		name      string
		mutate    func(*CreateBridgeInput)
		wantField string
	}{
		{"missing author", func(in *CreateBridgeInput) { in.AuthorID = "" }, "author_id"},
		{"author is not a uuid", func(in *CreateBridgeInput) { in.AuthorID = "not-a-uuid" }, "author_id"},
		{"empty body", func(in *CreateBridgeInput) { in.Body = "   " }, "body"},
		{"body too long", func(in *CreateBridgeInput) { in.Body = longBody }, "body"},
		{"unknown target language code", func(in *CreateBridgeInput) { in.TargetLanguage = "f" }, "target_language"},
		{"two letter target language", func(in *CreateBridgeInput) { in.TargetLanguage = "fr" }, "target_language"},
		{"target language given as a word", func(in *CreateBridgeInput) { in.TargetLanguage = "english" }, "target_language"},
		{"target language in upper case", func(in *CreateBridgeInput) { in.TargetLanguage = "FRA" }, "target_language"},
		{"target language with digits", func(in *CreateBridgeInput) { in.TargetLanguage = "fr2" }, "target_language"},
		{"adaptation note too long", func(in *CreateBridgeInput) { in.AdaptationNote = longNote }, "adaptation_note"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, comments, bridges := newTestService(t)
			comments.getResult = sourceComment()

			input := validBridgeInput()
			test.mutate(&input)

			_, _, _, err := service.CreateBridge(context.Background(), input)
			if err == nil {
				t.Fatal("CreateBridge() error = nil, want a validation error")
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
			if comments.getCalls != 0 || bridges.createCalls != 0 || bridges.findTargetCalls != 0 {
				t.Error("the store was called, want validation to run before any query")
			}
		})
	}
}

func TestCreateBridgeMalformedSourceIdIsNotFound(t *testing.T) {
	service, comments, bridges := newTestService(t)

	input := validBridgeInput()
	input.SourceCommentID = "not-a-uuid"

	_, _, _, err := service.CreateBridge(context.Background(), input)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if comments.getCalls != 0 || bridges.createCalls != 0 {
		t.Error("the store was called, want a malformed source id to stop before any query")
	}
}

func TestCreateBridgeSameLanguageIsRejected(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment() // language "eng"

	input := validBridgeInput()
	input.TargetLanguage = "eng"

	_, _, _, err := service.CreateBridge(context.Background(), input)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
	}
	if validation.Field != "target_language" {
		t.Errorf("field = %q, want %q", validation.Field, "target_language")
	}
	if bridges.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a same-language bridge must not be stored", bridges.createCalls)
	}
}

func TestCreateBridgeSourceNotFound(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getErr = ErrNotFound

	_, _, _, err := service.CreateBridge(context.Background(), validBridgeInput())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if bridges.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a missing source must not be bridged", bridges.createCalls)
	}
}

func TestCreateBridgeTargetVersionNotFound(t *testing.T) {
	// The story has no version in the target language, so there is no
	// conversation to bridge into.
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment()
	bridges.findTargetErr = ErrNotFound

	_, _, _, err := service.CreateBridge(context.Background(), validBridgeInput())
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
	}
	if validation.Field != "target_language" {
		t.Errorf("field = %q, want %q", validation.Field, "target_language")
	}
	if bridges.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — no target version means no bridge", bridges.createCalls)
	}
}

func TestCreateBridgeTargetLookupFailureIsWrapped(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment()
	bridges.findTargetErr = errors.New("connection reset")

	_, _, _, err := service.CreateBridge(context.Background(), validBridgeInput())
	if err == nil {
		t.Fatal("CreateBridge() error = nil, want the store error")
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than a domain one", err)
	}
}

func TestCreateBridgeAlreadyBridgedBecomesValidation(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment()
	bridges.createErr = ErrAlreadyBridged

	_, _, _, err := service.CreateBridge(context.Background(), validBridgeInput())
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
	}
	if validation.Field != "target_language" {
		t.Errorf("field = %q, want %q", validation.Field, "target_language")
	}
}

func TestCreateBridgeStoreFailureIsWrapped(t *testing.T) {
	service, comments, bridges := newTestService(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID
	bridges.createErr = errors.New("connection reset")

	_, _, _, err := service.CreateBridge(context.Background(), validBridgeInput())
	if err == nil {
		t.Fatal("CreateBridge() error = nil, want the store error")
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than a domain one", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want the underlying cause preserved", err)
	}
}

func TestGetBridgeReturnsStoreResult(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.getResult = Bridge{ID: bridgeID, SourceCommentID: sourceCommentID, TargetLanguage: "fra"}

	bridge, err := service.GetBridge(context.Background(), bridgeID)
	if err != nil {
		t.Fatalf("GetBridge() error = %v, want nil", err)
	}
	if bridge.ID != bridgeID || bridge.TargetLanguage != "fra" {
		t.Errorf("bridge = %+v, want the stored bridge", bridge)
	}
}

func TestGetBridgeMalformedIdIsNotFoundWithoutQueryingTheStore(t *testing.T) {
	service, _, bridges := newTestService(t)

	_, err := service.GetBridge(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if bridges.getCalls != 0 {
		t.Errorf("store received %d get calls, want 0 — a malformed id cannot match a row", bridges.getCalls)
	}
}

func TestGetBridgeNotFound(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.getErr = ErrNotFound

	_, err := service.GetBridge(context.Background(), bridgeID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestGetBridgeStoreFailureIsWrapped(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.getErr = errors.New("connection reset")

	_, err := service.GetBridge(context.Background(), bridgeID)
	if err == nil {
		t.Fatal("GetBridge() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}

func TestGetCommentReturnsStoreResult(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getResult = Comment{
		ID:        sourceCommentID,
		VersionID: versionID,
		StoryID:   storyID,
		AuthorID:  authorID,
		Language:  "eng",
		Body:      "The first rain remembers every name.",
	}

	comment, err := service.GetComment(context.Background(), sourceCommentID)
	if err != nil {
		t.Fatalf("GetComment() error = %v, want nil", err)
	}
	if comment.ID != sourceCommentID || comment.VersionID != versionID || comment.StoryID != storyID {
		t.Errorf("comment = %+v, want the stored comment with its story resolved", comment)
	}
}

func TestGetCommentMalformedIdIsNotFoundWithoutQueryingTheStore(t *testing.T) {
	service, comments, _ := newTestService(t)

	_, err := service.GetComment(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if comments.getCalls != 0 {
		t.Errorf("store received %d get calls, want 0 — a malformed id cannot match a row", comments.getCalls)
	}
}

func TestGetCommentNotFound(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getErr = ErrNotFound

	_, err := service.GetComment(context.Background(), sourceCommentID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestGetCommentStoreFailureIsWrapped(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getErr = errors.New("connection reset")

	_, err := service.GetComment(context.Background(), sourceCommentID)
	if err == nil {
		t.Fatal("GetComment() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}

func TestListBridgesForComment(t *testing.T) {
	tests := []struct {
		name    string
		bridges []Bridge
	}{
		{name: "empty", bridges: nil},
		{name: "one", bridges: []Bridge{{ID: bridgeID, SourceCommentID: sourceCommentID}}},
		{name: "many", bridges: []Bridge{
			{ID: bridgeID, SourceCommentID: sourceCommentID},
			{ID: bridgeID, TargetCommentID: sourceCommentID, TargetLanguage: "fra"},
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, bridges := newTestService(t)
			bridges.listForCommentResult = test.bridges

			got, err := service.ListBridgesForComment(context.Background(), sourceCommentID)
			if err != nil {
				t.Fatalf("ListBridgesForComment() error = %v, want nil", err)
			}
			if got == nil {
				t.Fatal("bridges = nil, want a non-nil slice so the response contains [] rather than null")
			}
			if len(got) != len(test.bridges) {
				t.Errorf("len(bridges) = %d, want %d", len(got), len(test.bridges))
			}
			if bridges.gotCommentID != sourceCommentID {
				t.Errorf("store received comment id %q, want %q", bridges.gotCommentID, sourceCommentID)
			}
		})
	}
}

func TestListBridgesForCommentMalformedIdIsNotFound(t *testing.T) {
	service, _, bridges := newTestService(t)

	_, err := service.ListBridgesForComment(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if bridges.listForCommentCalls != 0 {
		t.Errorf("store received %d calls, want 0", bridges.listForCommentCalls)
	}
}

func TestListBridgesForCommentNotFound(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.listForCommentErr = ErrNotFound

	_, err := service.ListBridgesForComment(context.Background(), sourceCommentID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestListBridgesForStory(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.listForStoryResult = []Bridge{{ID: bridgeID, SourceCommentID: sourceCommentID}}

	got, err := service.ListBridgesForStory(context.Background(), storyID)
	if err != nil {
		t.Fatalf("ListBridgesForStory() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Errorf("len(bridges) = %d, want 1", len(got))
	}
	if bridges.gotStoryID != storyID {
		t.Errorf("store received story id %q, want %q", bridges.gotStoryID, storyID)
	}
}

func TestListBridgesForStoryEmptyIsAnEmptySliceNotNil(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.listForStoryResult = nil

	got, err := service.ListBridgesForStory(context.Background(), storyID)
	if err != nil {
		t.Fatalf("ListBridgesForStory() error = %v, want nil", err)
	}
	if got == nil {
		t.Error("bridges = nil, want an empty slice so the response contains [] rather than null")
	}
}

func TestListBridgesForStoryNotFound(t *testing.T) {
	service, _, bridges := newTestService(t)
	bridges.listForStoryErr = ErrNotFound

	_, err := service.ListBridgesForStory(context.Background(), storyID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

// --- replies (KNOT-ADR-047) ------------------------------------------------

// parentPtr returns a pointer to id, for the optional parent field.
func parentPtr(id string) *string { return &id }

// validReplyInput is a comment by authorID that replies to parentID.
func validReplyInput(parentID string) CreateCommentInput {
	input := validCommentInput()
	input.ParentCommentID = parentPtr(parentID)
	return input
}

// requireFieldError asserts that err is a validation error naming field, which is
// how every rejected field is reported.
func requireFieldError(t *testing.T, err error, field string) {
	t.Helper()

	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("errors.As(err, &ValidationError) = false, want true (err = %v)", err)
	}
	if validation.Field != field {
		t.Errorf("field = %q, want %q", validation.Field, field)
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}
}

func TestCreateCommentReplyStoresTheParent(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getResult = sourceComment()

	created, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID))
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if comments.getCalls != 1 || comments.gotGetID != sourceCommentID {
		t.Errorf("parent lookups = %d (last id %q), want one lookup of %q", comments.getCalls, comments.gotGetID, sourceCommentID)
	}
	if comments.gotCreate.ParentCommentID == nil {
		t.Fatal("stored parent = nil, want the parent comment")
	}
	if *comments.gotCreate.ParentCommentID != sourceCommentID {
		t.Errorf("stored parent = %q, want %q", *comments.gotCreate.ParentCommentID, sourceCommentID)
	}
	if created.ID == "" {
		t.Error("id is empty, want the stored id")
	}
}

func TestCreateCommentWithoutAParentIsTopLevel(t *testing.T) {
	service, comments, _ := newTestService(t)

	if _, err := service.CreateComment(context.Background(), validCommentInput()); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if comments.gotCreate.ParentCommentID != nil {
		t.Errorf("stored parent = %q, want nil for a top-level comment", *comments.gotCreate.ParentCommentID)
	}
	if comments.getCalls != 0 {
		t.Errorf("parent lookups = %d, want 0 — a top-level comment has no parent to check", comments.getCalls)
	}
}

func TestCreateCommentReplyToAReplyAttachesToTheTopLevel(t *testing.T) {
	service, comments, _ := newTestService(t)

	// The comment being replied to is itself a reply, so the stored parent must be
	// that reply's own parent: a thread is one level deep, never two.
	reply := sourceComment()
	reply.ParentCommentID = parentPtr(targetCommentID)
	comments.getResult = reply

	if _, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID)); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if comments.gotCreate.ParentCommentID == nil {
		t.Fatal("stored parent = nil, want the top-level comment")
	}
	if *comments.gotCreate.ParentCommentID != targetCommentID {
		t.Errorf("stored parent = %q, want the top-level comment %q", *comments.gotCreate.ParentCommentID, targetCommentID)
	}
}

func TestCreateCommentReplyRejectsAParentOnAnotherVersion(t *testing.T) {
	service, comments, _ := newTestService(t)

	parent := sourceComment()
	parent.VersionID = otherVersionID
	comments.getResult = parent

	_, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID))
	requireFieldError(t, err, "parent_comment_id")

	if comments.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a reply to another version must not be stored", comments.createCalls)
	}
}

func TestCreateCommentReplyRejectsAnUnknownParent(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.getErr = ErrNotFound

	_, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID))
	requireFieldError(t, err, "parent_comment_id")

	if comments.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — an unknown parent must not be stored", comments.createCalls)
	}
}

func TestCreateCommentReplyToYourOwnCommentNotifiesNobody(t *testing.T) {
	service, comments, _, notifier := newTestServiceWithNotifier(t)
	comments.getResult = sourceComment() // authored by authorID, the commenter

	if _, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID)); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none — replying to yourself is not news", notifier.calls)
	}
}

func TestCreateCommentReplyNotifiesTheParentAuthorNotTheVersionAuthor(t *testing.T) {
	service, comments, _, notifier := newTestServiceWithNotifier(t)

	parent := sourceComment()
	parent.AuthorID = otherAuthorID
	comments.getResult = parent
	// The version's author is the commenter, so a version-targeted notification
	// would be suppressed. Only the parent's author can be the recipient here.
	comments.versionAuthorResult = authorID

	if _, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID)); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("notifications = %d, want exactly 1", len(notifier.calls))
	}
	call := notifier.calls[0]
	if call.recipientID != otherAuthorID {
		t.Errorf("recipient = %q, want the parent's author %q", call.recipientID, otherAuthorID)
	}
	if call.actorID != authorID {
		t.Errorf("actor = %q, want the commenter %q", call.actorID, authorID)
	}
	if call.kind != "comment.created" {
		t.Errorf("kind = %q, want %q — a reply is not a new event type", call.kind, "comment.created")
	}
}

func TestCreateCommentReplyToAReplyNotifiesTheAuthorRepliedTo(t *testing.T) {
	service, comments, _, notifier := newTestServiceWithNotifier(t)

	// The comment addressed is a reply by otherAuthorID; the stored parent becomes
	// its parent, but the notification goes to whoever was replied to.
	reply := sourceComment()
	reply.AuthorID = otherAuthorID
	reply.ParentCommentID = parentPtr(targetCommentID)
	comments.getResult = reply
	// The version's author is the commenter, so a version-targeted notification
	// would be suppressed rather than delivered to the wrong person.
	comments.versionAuthorResult = authorID

	if _, err := service.CreateComment(context.Background(), validReplyInput(sourceCommentID)); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("notifications = %d, want exactly 1", len(notifier.calls))
	}
	if got := notifier.calls[0].recipientID; got != otherAuthorID {
		t.Errorf("recipient = %q, want the author of the comment replied to %q", got, otherAuthorID)
	}
}

func TestListCommentsReturnsEachParentWithItsReplies(t *testing.T) {
	service, comments, _ := newTestService(t)

	newest := Comment{ID: targetCommentID, VersionID: versionID, AuthorID: authorID, Language: "eng", Body: "newest"}
	oldest := sourceComment()
	comments.listResult = []Comment{newest, oldest}

	firstReply := Comment{ID: "99999999-9999-4999-8999-999999999991", VersionID: versionID, AuthorID: authorID, Language: "eng", Body: "first reply", ParentCommentID: parentPtr(oldest.ID)}
	secondReply := Comment{ID: "99999999-9999-4999-8999-999999999992", VersionID: versionID, AuthorID: authorID, Language: "eng", Body: "second reply", ParentCommentID: parentPtr(oldest.ID)}
	// The store returns replies oldest first, and only for the page's parents.
	comments.repliesResult = []Comment{firstReply, secondReply}

	page, _, err := service.ListComments(context.Background(), versionID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
	}

	want := []string{newest.ID, oldest.ID, firstReply.ID, secondReply.ID}
	if len(page) != len(want) {
		t.Fatalf("comments = %d, want %d: %+v", len(page), len(want), page)
	}
	for i, id := range want {
		if page[i].ID != id {
			t.Errorf("comment %d = %q, want %q (page = %+v)", i, page[i].ID, id, page)
		}
	}

	if len(comments.gotReplyParentIDs) != 2 || comments.gotReplyParentIDs[0] != newest.ID || comments.gotReplyParentIDs[1] != oldest.ID {
		t.Errorf("reply lookup parents = %v, want the page's parents in page order", comments.gotReplyParentIDs)
	}
}

func TestListCommentsDoesNotAskForRepliesWhenThePageIsEmpty(t *testing.T) {
	service, comments, _ := newTestService(t)
	comments.listResult = []Comment{}

	page, _, err := service.ListComments(context.Background(), versionID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil", err)
	}
	if len(page) != 0 {
		t.Errorf("comments = %d, want 0", len(page))
	}
	if comments.replyCalls != 0 {
		t.Errorf("reply lookups = %d, want 0 — an empty page has no parents to hang replies on", comments.replyCalls)
	}
}

// ensure the fakes satisfy the domain contracts at compile time.
var (
	_ CommentStore = (*fakeCommentStore)(nil)
	_ BridgeStore  = (*fakeBridgeStore)(nil)
)
