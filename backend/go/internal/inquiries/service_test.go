package inquiries

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Fixture ids. They are canonical UUID text because the service validates them
// before a store sees them.
const (
	testAuthorID   = "11111111-1111-4111-8111-111111111111"
	testAnswererID = "22222222-2222-4222-8222-222222222222"
	testInquiryID  = "33333333-3333-4333-8333-333333333333"
	testAnswerID   = "44444444-4444-4444-8444-444444444444"
	testStrangerID = "55555555-5555-4555-8555-555555555555"
)

// fakeStore is an in-memory InquiryStore. It records the arguments it received so
// a test can assert what the service passed through, and returns canned results so
// the business rules can be tested without a database.
type fakeStore struct {
	createResult Inquiry
	createErr    error
	createCalls  int
	gotCreate    Inquiry

	getResult Inquiry
	getErr    error
	getCalls  int
	gotGetID  string

	listResult []Inquiry
	listNext   *Cursor
	listErr    error
	listCalls  int
	gotListCur *Cursor
	gotLimit   int
	gotPlace   string

	answerResult        Answer
	answerInquiryAuthor string
	answerErr           error
	answerCalls         int
	gotAnswer           Answer

	answersResult []Answer
	answersNext   *Cursor
	answersErr    error
	answersCalls  int
	gotAnswersID  string

	getAnswerResult Answer
	getAnswerErr    error
	getAnswerCalls  int
}

func (f *fakeStore) CreateInquiry(_ context.Context, inquiry Inquiry) (Inquiry, error) {
	f.createCalls++
	f.gotCreate = inquiry
	return f.createResult, f.createErr
}

func (f *fakeStore) GetInquiry(_ context.Context, id string) (Inquiry, error) {
	f.getCalls++
	f.gotGetID = id
	return f.getResult, f.getErr
}

func (f *fakeStore) ListInquiries(_ context.Context, cursor *Cursor, limit int, place string) ([]Inquiry, *Cursor, error) {
	f.listCalls++
	f.gotListCur = cursor
	f.gotLimit = limit
	f.gotPlace = place
	return f.listResult, f.listNext, f.listErr
}

func (f *fakeStore) CreateAnswer(_ context.Context, answer Answer) (Answer, string, error) {
	f.answerCalls++
	f.gotAnswer = answer
	return f.answerResult, f.answerInquiryAuthor, f.answerErr
}

func (f *fakeStore) ListAnswers(_ context.Context, inquiryID string, cursor *Cursor, limit int) ([]Answer, *Cursor, error) {
	f.answersCalls++
	f.gotAnswersID = inquiryID
	f.gotListCur = cursor
	f.gotLimit = limit
	return f.answersResult, f.answersNext, f.answersErr
}

func (f *fakeStore) GetAnswer(_ context.Context, id string) (Answer, error) {
	f.getAnswerCalls++
	f.gotGetID = id
	return f.getAnswerResult, f.getAnswerErr
}

// fakeRouting is the RootedRouting lookup. It returns a canned recipient list, so
// the routing behaviour can be tested without the Rooted domain.
type fakeRouting struct {
	ids       []string
	err       error
	calls     int
	gotPlace  string
	gotLimit  int
	gotCalled bool
}

func (f *fakeRouting) RootedUserIDsByPlace(_ context.Context, place string, limit int) ([]string, error) {
	f.calls++
	f.gotCalled = true
	f.gotPlace = place
	f.gotLimit = limit
	return f.ids, f.err
}

// notified records one notification the service fired.
type notified struct {
	event       string
	recipientID string
	actorID     string
	inquiryID   string
}

// fakeNotifier records every notification and can be made to fail, to prove
// that a notification failure never fails the request that caused it.
type fakeNotifier struct {
	answered    []notified
	nearby      []notified
	answeredErr error
	nearbyErr   error
}

func (f *fakeNotifier) NotifyInquiryAnswered(_ context.Context, recipientID, actorID, inquiryID string) error {
	f.answered = append(f.answered, notified{event: "inquiry.answered", recipientID: recipientID, actorID: actorID, inquiryID: inquiryID})
	return f.answeredErr
}

func (f *fakeNotifier) NotifyInquiryNearby(_ context.Context, recipientID, actorID, inquiryID string) error {
	f.nearby = append(f.nearby, notified{event: "inquiry.nearby", recipientID: recipientID, actorID: actorID, inquiryID: inquiryID})
	return f.nearbyErr
}

// discardLogger returns a logger that writes nowhere, so a test's output stays
// readable.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestService builds a service over the given stubs, defaulting the ones a test
// does not exercise.
func newTestService(t *testing.T, store InquiryStore, routing RootedRouting, notifier Notifier) *Service {
	t.Helper()

	if store == nil {
		store = &fakeStore{}
	}
	if routing == nil {
		routing = &fakeRouting{}
	}
	if notifier == nil {
		notifier = &fakeNotifier{}
	}

	service, err := NewService(store, routing, notifier, discardLogger())
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}
	return service
}

func validInquiryInput() CreateInquiryInput {
	return CreateInquiryInput{
		Title:    "Why do the cattle come home at the same hour?",
		Body:     "Every evening, without anyone calling them.",
		Language: "eng",
		Place:    "Manguzi",
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeRouting{}, &fakeNotifier{}, discardLogger()); err == nil {
		t.Error("NewService(nil store) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, nil, &fakeNotifier{}, discardLogger()); err == nil {
		t.Error("NewService(nil routing) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, &fakeRouting{}, nil, discardLogger()); err == nil {
		t.Error("NewService(nil notifier) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, &fakeRouting{}, &fakeNotifier{}, nil); err == nil {
		t.Error("NewService(nil logger) error = nil, want an error")
	}
}

func TestCreateInquiryTrimsAndStores(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	routing := &fakeRouting{}

	service := newTestService(t, store, routing, nil)

	stored, err := service.CreateInquiry(context.Background(), testAuthorID, validInquiryInput())
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
	if stored.ID != testInquiryID {
		t.Errorf("ID = %q, want %q", stored.ID, testInquiryID)
	}
	if store.gotCreate.AuthorID != testAuthorID {
		t.Errorf("author = %q, want %q", store.gotCreate.AuthorID, testAuthorID)
	}
	if store.gotCreate.Place == nil || *store.gotCreate.Place != "Manguzi" {
		t.Errorf("place = %v, want Manguzi", store.gotCreate.Place)
	}
}

func TestCreateInquiryWithoutPlaceIsNotRouted(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	routing := &fakeRouting{}
	notifier := &fakeNotifier{}

	service := newTestService(t, store, routing, notifier)

	input := validInquiryInput()
	input.Place = ""

	if _, err := service.CreateInquiry(context.Background(), testAuthorID, input); err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
	if store.gotCreate.Place != nil {
		t.Errorf("place = %v, want nil", store.gotCreate.Place)
	}
	// A question about nowhere routes nowhere: the Rooted lookup is not even asked.
	if routing.gotCalled {
		t.Error("routing lookup was called for a place-less inquiry")
	}
	if len(notifier.nearby) != 0 {
		t.Errorf("nearby notifications = %d, want 0", len(notifier.nearby))
	}
}

func TestCreateInquiryRoutesToFirstRootedUsersAndSkipsTheAsker(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	// The asker is themselves Rooted in the place, which is ordinary: you ask about
	// a place you know. They must not be notified of their own question.
	routing := &fakeRouting{ids: []string{testStrangerID, testAuthorID, testAnswererID}}
	notifier := &fakeNotifier{}

	service := newTestService(t, store, routing, notifier)

	if _, err := service.CreateInquiry(context.Background(), testAuthorID, validInquiryInput()); err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	if routing.gotPlace != "Manguzi" {
		t.Errorf("routed place = %q, want Manguzi", routing.gotPlace)
	}
	if routing.gotLimit != NearbyRecipientLimit {
		t.Errorf("routed limit = %d, want %d", routing.gotLimit, NearbyRecipientLimit)
	}
	if len(notifier.nearby) != 2 {
		t.Fatalf("nearby notifications = %d, want 2", len(notifier.nearby))
	}
	for _, item := range notifier.nearby {
		if item.recipientID == testAuthorID {
			t.Error("the asker was notified of their own inquiry")
		}
		if item.actorID != testAuthorID {
			t.Errorf("actor = %q, want the asker %q", item.actorID, testAuthorID)
		}
		if item.inquiryID != testInquiryID {
			t.Errorf("inquiry id = %q, want %q", item.inquiryID, testInquiryID)
		}
	}
}

// Routing is a courtesy to the question, not part of it: a Rooted lookup that
// fails must not turn a stored question into a failed request.
func TestCreateInquirySucceedsWhenRoutingFails(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	routing := &fakeRouting{err: errors.New("rooted is unavailable")}
	notifier := &fakeNotifier{}

	service := newTestService(t, store, routing, notifier)

	stored, err := service.CreateInquiry(context.Background(), testAuthorID, validInquiryInput())
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
	if stored.ID != testInquiryID {
		t.Errorf("ID = %q, want %q", stored.ID, testInquiryID)
	}
	if len(notifier.nearby) != 0 {
		t.Errorf("nearby notifications = %d, want 0 when routing failed", len(notifier.nearby))
	}
}

func TestCreateInquirySucceedsWhenNotificationFails(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	routing := &fakeRouting{ids: []string{testStrangerID}}
	notifier := &fakeNotifier{nearbyErr: errors.New("inbox is unavailable")}

	service := newTestService(t, store, routing, notifier)

	if _, err := service.CreateInquiry(context.Background(), testAuthorID, validInquiryInput()); err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
}

func TestCreateInquiryRejectsBadAuthor(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, nil, nil)

	if _, err := service.CreateInquiry(context.Background(), "not-a-uuid", validInquiryInput()); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("CreateInquiry() error = %v, want ErrUserNotFound", err)
	}
	if store.createCalls != 0 {
		t.Errorf("store create calls = %d, want 0", store.createCalls)
	}
}

func TestCreateInquiryUnknownAuthorIsUserNotFound(t *testing.T) {
	store := &fakeStore{createErr: ErrUserNotFound}
	service := newTestService(t, store, nil, nil)

	if _, err := service.CreateInquiry(context.Background(), testAuthorID, validInquiryInput()); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("CreateInquiry() error = %v, want ErrUserNotFound", err)
	}
}

func TestCreateInquiryValidation(t *testing.T) {
	longTitle := strings.Repeat("a", MaxTitleLength+1)
	longBody := strings.Repeat("a", MaxBodyLength+1)
	longPlace := strings.Repeat("a", MaxPlaceLength+1)
	longCountry := strings.Repeat("a", MaxPlaceCountryLength+1)
	latitude := 1.0

	cases := []struct {
		name  string
		field string
		edit  func(in *CreateInquiryInput)
	}{
		{name: "empty title", field: "title", edit: func(in *CreateInquiryInput) { in.Title = "   " }},
		{name: "long title", field: "title", edit: func(in *CreateInquiryInput) { in.Title = longTitle }},
		{name: "empty body", field: "body", edit: func(in *CreateInquiryInput) { in.Body = "\n\t " }},
		{name: "long body", field: "body", edit: func(in *CreateInquiryInput) { in.Body = longBody }},
		{name: "unknown language", field: "language", edit: func(in *CreateInquiryInput) { in.Language = "zzz" }},
		{name: "empty language", field: "language", edit: func(in *CreateInquiryInput) { in.Language = "" }},
		{name: "long place", field: "place", edit: func(in *CreateInquiryInput) { in.Place = longPlace }},
		{name: "multi-line place", field: "place", edit: func(in *CreateInquiryInput) { in.Place = "Manguzi\nStreet 5" }},
		{name: "interior tab in place", field: "place", edit: func(in *CreateInquiryInput) { in.Place = "Manguzi\tBay" }},
		{name: "long country", field: "place_country", edit: func(in *CreateInquiryInput) { in.PlaceCountry = longCountry }},
		{name: "lone latitude", field: "latitude", edit: func(in *CreateInquiryInput) { in.Latitude = &latitude }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeStore{}
			service := newTestService(t, store, nil, nil)

			input := validInquiryInput()
			testCase.edit(&input)

			_, err := service.CreateInquiry(context.Background(), testAuthorID, input)
			if err == nil {
				t.Fatal("CreateInquiry() error = nil, want a validation error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("CreateInquiry() error = %v, want it to satisfy ErrValidation", err)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("CreateInquiry() error = %v, want a *ValidationError", err)
			}
			if validation.Field != testCase.field {
				t.Errorf("field = %q, want %q", validation.Field, testCase.field)
			}
			if store.createCalls != 0 {
				t.Errorf("store create calls = %d, want 0", store.createCalls)
			}
		})
	}
}

func TestCreateInquiryAcceptsBoundaryLengths(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	service := newTestService(t, store, nil, nil)

	input := validInquiryInput()
	input.Title = strings.Repeat("a", MaxTitleLength)
	input.Body = strings.Repeat("a", MaxBodyLength)
	input.Place = strings.Repeat("a", MaxPlaceLength)

	if _, err := service.CreateInquiry(context.Background(), testAuthorID, input); err != nil {
		t.Fatalf("CreateInquiry() at the exact maximum error = %v, want nil", err)
	}
}

func TestCreateInquiryAcceptsAValidCoordinatePair(t *testing.T) {
	store := &fakeStore{createResult: Inquiry{ID: testInquiryID, AuthorID: testAuthorID}}
	service := newTestService(t, store, nil, nil)

	latitude, longitude := -26.9, 32.7
	input := validInquiryInput()
	input.Latitude = &latitude
	input.Longitude = &longitude

	if _, err := service.CreateInquiry(context.Background(), testAuthorID, input); err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
	if store.gotCreate.Latitude == nil || *store.gotCreate.Latitude != latitude {
		t.Errorf("latitude = %v, want %v", store.gotCreate.Latitude, latitude)
	}
}

func TestCreateInquiryRejectsOutOfRangeCoordinate(t *testing.T) {
	latitude, longitude := 91.0, 32.7
	input := validInquiryInput()
	input.Latitude = &latitude
	input.Longitude = &longitude

	service := newTestService(t, &fakeStore{}, nil, nil)

	_, err := service.CreateInquiry(context.Background(), testAuthorID, input)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("CreateInquiry() error = %v, want ErrValidation", err)
	}
}

func TestGetInquiryRejectsMalformedID(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, nil, nil)

	if _, err := service.GetInquiry(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetInquiry() error = %v, want ErrNotFound", err)
	}
	if store.getCalls != 0 {
		t.Errorf("store get calls = %d, want 0", store.getCalls)
	}
}

func TestGetInquiryPassesStoreNotFoundThrough(t *testing.T) {
	store := &fakeStore{getErr: ErrNotFound}
	service := newTestService(t, store, nil, nil)

	if _, err := service.GetInquiry(context.Background(), testInquiryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetInquiry() error = %v, want ErrNotFound", err)
	}
}

func TestListInquiriesTrimsThePlaceFilter(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, nil, nil)

	if _, _, err := service.ListInquiries(context.Background(), "", DefaultListLimit, "  Manguzi  "); err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}
	if store.gotPlace != "Manguzi" {
		t.Errorf("place = %q, want the trimmed Manguzi", store.gotPlace)
	}
}

func TestListInquiriesEmptyResultIsNeverNil(t *testing.T) {
	store := &fakeStore{listResult: nil}
	service := newTestService(t, store, nil, nil)

	page, next, err := service.ListInquiries(context.Background(), "", DefaultListLimit, "")
	if err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}
	if page == nil {
		t.Error("page = nil, want an empty slice so the wire form is []")
	}
	if next != "" {
		t.Errorf("next = %q, want the empty string at the end", next)
	}
}

func TestListInquiriesEncodesAndDecodesTheCursor(t *testing.T) {
	createdAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	next := NewCursor(createdAt, testInquiryID)
	store := &fakeStore{listNext: &next}
	service := newTestService(t, store, nil, nil)

	_, encoded, err := service.ListInquiries(context.Background(), "", DefaultListLimit, "")
	if err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}
	if encoded != next.Encode() {
		t.Errorf("next cursor = %q, want %q", encoded, next.Encode())
	}

	// The token the service issued is one it accepts back.
	if _, _, err := service.ListInquiries(context.Background(), encoded, DefaultListLimit, ""); err != nil {
		t.Fatalf("ListInquiries(with cursor) error = %v, want nil", err)
	}
	if store.gotListCur == nil {
		t.Fatal("store received a nil cursor, want the decoded one")
	}
	if !store.gotListCur.CreatedAt().Equal(createdAt) || store.gotListCur.ID() != testInquiryID {
		t.Errorf("decoded cursor = %v/%q, want %v/%q", store.gotListCur.CreatedAt(), store.gotListCur.ID(), createdAt, testInquiryID)
	}
}

func TestListInquiriesRejectsBadCursorAndLimit(t *testing.T) {
	service := newTestService(t, &fakeStore{}, nil, nil)

	if _, _, err := service.ListInquiries(context.Background(), "!!!not-base64!!!", DefaultListLimit, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("bad cursor error = %v, want ErrValidation", err)
	}
	if _, _, err := service.ListInquiries(context.Background(), "", 0, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("zero limit error = %v, want ErrValidation", err)
	}
	if _, _, err := service.ListInquiries(context.Background(), "", MaxListLimit+1, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("over-limit error = %v, want ErrValidation", err)
	}
}

func TestCreateAnswerStoresAndNotifiesTheAsker(t *testing.T) {
	store := &fakeStore{
		answerResult:        Answer{ID: testAnswerID, InquiryID: testInquiryID, AuthorID: testAnswererID},
		answerInquiryAuthor: testAuthorID,
	}
	notifier := &fakeNotifier{}

	service := newTestService(t, store, nil, notifier)

	stored, err := service.CreateAnswer(context.Background(), testInquiryID, testAnswererID, CreateAnswerInput{
		Body:     "  They follow the river.  ",
		Language: "eng",
	})
	if err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}
	if stored.ID != testAnswerID {
		t.Errorf("ID = %q, want %q", stored.ID, testAnswerID)
	}
	// The body is trimmed before it reaches the store.
	if store.gotAnswer.Body != "They follow the river." {
		t.Errorf("stored body = %q, want the trimmed body", store.gotAnswer.Body)
	}
	if store.gotAnswer.InquiryID != testInquiryID || store.gotAnswer.AuthorID != testAnswererID {
		t.Errorf("stored answer ids = %q/%q, want %q/%q", store.gotAnswer.InquiryID, store.gotAnswer.AuthorID, testInquiryID, testAnswererID)
	}

	if len(notifier.answered) != 1 {
		t.Fatalf("answered notifications = %d, want 1", len(notifier.answered))
	}
	got := notifier.answered[0]
	if got.recipientID != testAuthorID || got.actorID != testAnswererID || got.inquiryID != testInquiryID {
		t.Errorf("notification = %+v, want recipient %q actor %q inquiry %q", got, testAuthorID, testAnswererID, testInquiryID)
	}
}

// Answering your own question is ordinary and must not notify you, because acting
// on your own content never notifies you (KNOT-ADR-038).
func TestCreateAnswerOnOwnInquiryDoesNotNotify(t *testing.T) {
	store := &fakeStore{
		answerResult:        Answer{ID: testAnswerID, InquiryID: testInquiryID, AuthorID: testAuthorID},
		answerInquiryAuthor: testAuthorID,
	}
	notifier := &fakeNotifier{}

	service := newTestService(t, store, nil, notifier)

	if _, err := service.CreateAnswer(context.Background(), testInquiryID, testAuthorID, CreateAnswerInput{Body: "I looked it up.", Language: "eng"}); err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}
	if len(notifier.answered) != 0 {
		t.Errorf("answered notifications = %d, want 0 for a self-answer", len(notifier.answered))
	}
}

func TestCreateAnswerSucceedsWhenNotificationFails(t *testing.T) {
	store := &fakeStore{
		answerResult:        Answer{ID: testAnswerID, InquiryID: testInquiryID, AuthorID: testAnswererID},
		answerInquiryAuthor: testAuthorID,
	}
	notifier := &fakeNotifier{answeredErr: errors.New("inbox is unavailable")}

	service := newTestService(t, store, nil, notifier)

	if _, err := service.CreateAnswer(context.Background(), testInquiryID, testAnswererID, CreateAnswerInput{Body: "They follow the river.", Language: "eng"}); err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}
}

func TestCreateAnswerRejectsBadIds(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, nil, nil)

	if _, err := service.CreateAnswer(context.Background(), "nope", testAnswererID, CreateAnswerInput{Body: "x", Language: "eng"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad inquiry id error = %v, want ErrNotFound", err)
	}
	if _, err := service.CreateAnswer(context.Background(), testInquiryID, "nope", CreateAnswerInput{Body: "x", Language: "eng"}); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("bad author id error = %v, want ErrUserNotFound", err)
	}
	if store.answerCalls != 0 {
		t.Errorf("store answer calls = %d, want 0", store.answerCalls)
	}
}

func TestCreateAnswerPassesStoreErrorsThrough(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "missing inquiry", err: ErrNotFound, want: ErrNotFound},
		{name: "missing author", err: ErrUserNotFound, want: ErrUserNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeStore{answerErr: testCase.err}
			service := newTestService(t, store, nil, nil)

			_, err := service.CreateAnswer(context.Background(), testInquiryID, testAnswererID, CreateAnswerInput{Body: "x", Language: "eng"})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("CreateAnswer() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestCreateAnswerValidation(t *testing.T) {
	long := strings.Repeat("a", MaxAnswerBodyLength+1)

	cases := []struct {
		name  string
		field string
		input CreateAnswerInput
	}{
		{name: "empty body", field: "body", input: CreateAnswerInput{Body: "   ", Language: "eng"}},
		{name: "long body", field: "body", input: CreateAnswerInput{Body: long, Language: "eng"}},
		{name: "unknown language", field: "language", input: CreateAnswerInput{Body: "ok", Language: "zzz"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := newTestService(t, &fakeStore{}, nil, nil)

			_, err := service.CreateAnswer(context.Background(), testInquiryID, testAnswererID, testCase.input)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("CreateAnswer() error = %v, want ErrValidation", err)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("CreateAnswer() error = %v, want a *ValidationError", err)
			}
			if validation.Field != testCase.field {
				t.Errorf("field = %q, want %q", validation.Field, testCase.field)
			}
		})
	}
}

// An unknown inquiry is a 404 rather than an empty thread, which would be
// indistinguishable from a question nobody has answered.
func TestListAnswersChecksTheInquiryExists(t *testing.T) {
	store := &fakeStore{getErr: ErrNotFound}
	service := newTestService(t, store, nil, nil)

	_, _, err := service.ListAnswers(context.Background(), testInquiryID, "", DefaultAnswerListLimit)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListAnswers() error = %v, want ErrNotFound", err)
	}
	if store.answersCalls != 0 {
		t.Errorf("store answer-list calls = %d, want 0 when the inquiry is missing", store.answersCalls)
	}
}

func TestListAnswersRejectsMalformedIDAndLimit(t *testing.T) {
	service := newTestService(t, &fakeStore{}, nil, nil)

	if _, _, err := service.ListAnswers(context.Background(), "nope", "", DefaultAnswerListLimit); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id error = %v, want ErrNotFound", err)
	}
	if _, _, err := service.ListAnswers(context.Background(), testInquiryID, "", 0); !errors.Is(err, ErrValidation) {
		t.Errorf("zero limit error = %v, want ErrValidation", err)
	}
	if _, _, err := service.ListAnswers(context.Background(), testInquiryID, "", MaxAnswerListLimit+1); !errors.Is(err, ErrValidation) {
		t.Errorf("over-limit error = %v, want ErrValidation", err)
	}
}

func TestListAnswersEmptyResultIsNeverNil(t *testing.T) {
	store := &fakeStore{getResult: Inquiry{ID: testInquiryID}, answersResult: nil}
	service := newTestService(t, store, nil, nil)

	page, next, err := service.ListAnswers(context.Background(), testInquiryID, "", DefaultAnswerListLimit)
	if err != nil {
		t.Fatalf("ListAnswers() error = %v, want nil", err)
	}
	if page == nil {
		t.Error("page = nil, want an empty slice so the wire form is []")
	}
	if next != "" {
		t.Errorf("next = %q, want the empty string at the end", next)
	}
	if store.gotAnswersID != testInquiryID {
		t.Errorf("store inquiry id = %q, want %q", store.gotAnswersID, testInquiryID)
	}
}

func TestGetAnswerRejectsMalformedID(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, nil, nil)

	if _, err := service.GetAnswer(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAnswer() error = %v, want ErrNotFound", err)
	}
	if store.getAnswerCalls != 0 {
		t.Errorf("store get answer calls = %d, want 0", store.getAnswerCalls)
	}
}

func TestGetAnswerReturnsTheStoredAnswer(t *testing.T) {
	store := &fakeStore{getAnswerResult: Answer{ID: testAnswerID, InquiryID: testInquiryID}}
	service := newTestService(t, store, nil, nil)

	answer, err := service.GetAnswer(context.Background(), testAnswerID)
	if err != nil {
		t.Fatalf("GetAnswer() error = %v, want nil", err)
	}
	if answer.ID != testAnswerID {
		t.Errorf("ID = %q, want %q", answer.ID, testAnswerID)
	}
}
