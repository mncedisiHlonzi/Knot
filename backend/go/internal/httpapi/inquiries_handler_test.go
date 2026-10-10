package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/inquiries"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/rooted"
)

// Test fixtures. The ids are canonical UUID text because every handler route
// validates them before touching a store.
const (
	testInquiryID       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testInquiryAnswerID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testInquiryAuthorID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	testAnswerAuthorID  = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	testInquiryReaderID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
)

// fakeInquiriesService is an in-memory InquiriesService. It records the arguments
// of each call so a test can assert what the handler passed through, and returns
// canned results so the handler can be tested without a database.
type fakeInquiriesService struct {
	createResult inquiries.Inquiry
	createErr    error
	createCalls  int
	gotAuthorID  string
	gotCreate    inquiries.CreateInquiryInput

	getResult inquiries.Inquiry
	getErr    error
	getCalls  int
	gotGetID  string

	listResult []inquiries.Inquiry
	listNext   string
	listErr    error
	listCalls  int
	gotCursor  string
	gotLimit   int
	gotPlace   string

	createAnswerResult inquiries.Answer
	createAnswerErr    error
	createAnswerCalls  int
	gotAnswerInquiryID string
	gotAnswerAuthorID  string
	gotAnswer          inquiries.CreateAnswerInput

	listAnswersResult []inquiries.Answer
	listAnswersNext   string
	listAnswersErr    error
	listAnswersCalls  int

	getAnswerResult inquiries.Answer
	getAnswerErr    error
	getAnswerCalls  int
}

func (f *fakeInquiriesService) CreateInquiry(_ context.Context, authorID string, in inquiries.CreateInquiryInput) (inquiries.Inquiry, error) {
	f.createCalls++
	f.gotAuthorID = authorID
	f.gotCreate = in
	return f.createResult, f.createErr
}

func (f *fakeInquiriesService) GetInquiry(_ context.Context, id string) (inquiries.Inquiry, error) {
	f.getCalls++
	f.gotGetID = id
	return f.getResult, f.getErr
}

func (f *fakeInquiriesService) ListInquiries(_ context.Context, rawCursor string, limit int, place string) ([]inquiries.Inquiry, string, error) {
	f.listCalls++
	f.gotCursor = rawCursor
	f.gotLimit = limit
	f.gotPlace = place
	return f.listResult, f.listNext, f.listErr
}

func (f *fakeInquiriesService) CreateAnswer(_ context.Context, inquiryID, authorID string, in inquiries.CreateAnswerInput) (inquiries.Answer, error) {
	f.createAnswerCalls++
	f.gotAnswerInquiryID = inquiryID
	f.gotAnswerAuthorID = authorID
	f.gotAnswer = in
	return f.createAnswerResult, f.createAnswerErr
}

func (f *fakeInquiriesService) ListAnswers(_ context.Context, inquiryID, rawCursor string, limit int) ([]inquiries.Answer, string, error) {
	f.listAnswersCalls++
	f.gotGetID = inquiryID
	f.gotCursor = rawCursor
	f.gotLimit = limit
	return f.listAnswersResult, f.listAnswersNext, f.listAnswersErr
}

func (f *fakeInquiriesService) GetAnswer(_ context.Context, id string) (inquiries.Answer, error) {
	f.getAnswerCalls++
	f.gotGetID = id
	return f.getAnswerResult, f.getAnswerErr
}

// newTestInquiriesHandler returns an inquiries handler over the given fake service,
// defaulting the enrichment lookups so a test only sets what it exercises.
func newTestInquiriesHandler(t *testing.T, logger *slog.Logger) *InquiriesHandler {
	t.Helper()

	handler, err := NewInquiriesHandler(
		&fakeInquiriesService{},
		&fakeAuthorService{users: map[string]*identity.User{}},
		&fakeRootedService{},
		&fakeReactionsLookup{},
		logger,
	)
	if err != nil {
		t.Fatalf("NewInquiriesHandler() error = %v, want nil", err)
	}
	return handler
}

// newInquiriesHandler builds a handler over the given service and stubs.
func newInquiriesHandler(t *testing.T, service InquiriesService, authors AuthorLookup, rootedLookup RootedLookup, reactionLookup ReactionsLookup) *InquiriesHandler {
	t.Helper()

	handler, err := NewInquiriesHandler(service, authors, rootedLookup, reactionLookup, discardLogger())
	if err != nil {
		t.Fatalf("NewInquiriesHandler() error = %v, want nil", err)
	}
	return handler
}

// doInquiryRequest calls an inquiries handler method directly, with the {id} path
// value and the given user on the context.
func doInquiryRequest(handler func(http.ResponseWriter, *http.Request), method, path, id, userID, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if id != "" {
		request.SetPathValue("id", id)
	}
	if userID != "" {
		request = request.WithContext(withUserID(request.Context(), userID))
	}

	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestCreateInquiryTakesAuthorFromTokenAndEnriches(t *testing.T) {
	inquiry := inquiries.Inquiry{
		ID:          testInquiryID,
		AuthorID:    testInquiryAuthorID,
		Title:       "Why do the cattle come home at the same hour?",
		Body:        "Every evening, the same time.",
		Language:    "eng",
		AnswerCount: 0,
		CreatedAt:   time.Now().UTC(),
	}
	service := &fakeInquiriesService{createResult: inquiry}
	authors := &fakeAuthorService{users: map[string]*identity.User{
		testInquiryAuthorID: {ID: testInquiryAuthorID, DisplayName: "Ada Lovelace"},
	}}
	rootedLookup := &fakeRootedService{batchResult: map[string]*rooted.Signal{
		testInquiryAuthorID: {UserID: testInquiryAuthorID, Place: "Manguzi", DurationBucket: rooted.DurationLifelong},
	}}
	reactionLookup := &fakeReactionsLookup{summaries: map[string]reactions.Summary{
		testInquiryID: {AddsSomethingNew: 1},
	}}

	handler := newInquiriesHandler(t, service, authors, rootedLookup, reactionLookup)

	recorder := doInquiryRequest(
		handler.Create,
		http.MethodPost,
		"/inquiries",
		"",
		testInquiryReaderID,
		`{"title":"Why do the cattle come home at the same hour?","body":"Every evening.","language":"eng","place":"Manguzi"}`,
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	// The asker is the token's subject, never the body: there is no author_id field
	// to spoof.
	if service.gotAuthorID != testInquiryReaderID {
		t.Errorf("create author = %q, want %q", service.gotAuthorID, testInquiryReaderID)
	}
	if service.gotCreate.Place != "Manguzi" {
		t.Errorf("create place = %q, want Manguzi", service.gotCreate.Place)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		`"author_display_name":"Ada Lovelace"`,
		`"duration_bucket":"lifelong"`,
		`"adds_something_new":1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response body %s missing %s", body, want)
		}
	}
}

func TestCreateInquiryRequiresAuthentication(t *testing.T) {
	service := &fakeInquiriesService{}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.Create, http.MethodPost, "/inquiries", "", "", `{"title":"x","body":"y","language":"eng"}`)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if service.createCalls != 0 {
		t.Errorf("service create calls = %d, want 0", service.createCalls)
	}
}

func TestCreateInquiryValidationErrorIsBadRequest(t *testing.T) {
	service := &fakeInquiriesService{
		createErr: &inquiries.ValidationError{Field: "title", Message: "is required"},
	}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.Create, http.MethodPost, "/inquiries", "", testInquiryReaderID, `{"title":"","body":"y","language":"eng"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "title is required") {
		t.Errorf("body = %s, want it to name the field and message", recorder.Body.String())
	}
}

func TestCreateInquiryUnknownAuthorIsNotFound(t *testing.T) {
	service := &fakeInquiriesService{createErr: inquiries.ErrUserNotFound}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.Create, http.MethodPost, "/inquiries", "", testInquiryReaderID, `{"title":"x","body":"y","language":"eng"}`)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestListInquiriesPassesPlaceFilterAndDefaultsLimit(t *testing.T) {
	service := &fakeInquiriesService{
		listResult: []inquiries.Inquiry{{ID: testInquiryID, AuthorID: testInquiryAuthorID, Title: "Why?"}},
	}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	request := httptest.NewRequest(http.MethodGet, "/inquiries?place=Manguzi", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotPlace != "Manguzi" {
		t.Errorf("place = %q, want Manguzi", service.gotPlace)
	}
	if service.gotLimit != inquiries.DefaultListLimit {
		t.Errorf("limit = %d, want %d", service.gotLimit, inquiries.DefaultListLimit)
	}
	if !strings.Contains(recorder.Body.String(), `"inquiries":[`) {
		t.Errorf("body = %s, want an inquiries array", recorder.Body.String())
	}
}

func TestListInquiriesClampsLimitAboveMaximum(t *testing.T) {
	service := &fakeInquiriesService{}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	request := httptest.NewRequest(http.MethodGet, "/inquiries?limit=500", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotLimit != inquiries.MaxListLimit {
		t.Errorf("limit = %d, want %d", service.gotLimit, inquiries.MaxListLimit)
	}
}

func TestListInquiriesRejectsMalformedLimit(t *testing.T) {
	service := &fakeInquiriesService{}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	request := httptest.NewRequest(http.MethodGet, "/inquiries?limit=zero", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if service.listCalls != 0 {
		t.Errorf("service list calls = %d, want 0", service.listCalls)
	}
}

func TestListInquiriesEmptyPageIsAnArray(t *testing.T) {
	service := &fakeInquiriesService{listResult: nil}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	request := httptest.NewRequest(http.MethodGet, "/inquiries", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if !strings.Contains(recorder.Body.String(), `"inquiries":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", recorder.Body.String())
	}
}

func TestGetInquiryEnrichesReactionsForTheReader(t *testing.T) {
	inquiry := inquiries.Inquiry{ID: testInquiryID, AuthorID: testInquiryAuthorID, Title: "Why?"}
	service := &fakeInquiriesService{getResult: inquiry}
	reactionLookup := &fakeReactionsLookup{
		summaries: map[string]reactions.Summary{testInquiryID: {RingsTrue: 2}},
		mine:      map[string][]reactions.ReactionType{testInquiryID: {reactions.RingsTrue}},
	}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, reactionLookup)

	recorder := doInquiryRequest(handler.Get, http.MethodGet, "/inquiries/x", testInquiryID, testInquiryReaderID, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	// Optional auth: the reader's own signals are named because a user is on the
	// context, and the entity type is the inquiry.
	if reactionLookup.gotUserID != testInquiryReaderID {
		t.Errorf("reaction reader = %q, want %q", reactionLookup.gotUserID, testInquiryReaderID)
	}
	if reactionLookup.gotEntityType != reactions.EntityInquiry {
		t.Errorf("reaction entity type = %q, want %q", reactionLookup.gotEntityType, reactions.EntityInquiry)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"rings_true":2`) || !strings.Contains(body, `"my_reactions":["rings_true"]`) {
		t.Errorf("body = %s, want the counts and the reader's own signal", body)
	}
}

func TestGetInquiryUnknownIsNotFound(t *testing.T) {
	service := &fakeInquiriesService{getErr: inquiries.ErrNotFound}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.Get, http.MethodGet, "/inquiries/x", testInquiryID, "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestCreateAnswerTakesAuthorFromToken(t *testing.T) {
	answer := inquiries.Answer{
		ID:        testInquiryAnswerID,
		InquiryID: testInquiryID,
		AuthorID:  testAnswerAuthorID,
		Language:  "eng",
		Body:      "They follow the river.",
		CreatedAt: time.Now().UTC(),
	}
	service := &fakeInquiriesService{createAnswerResult: answer}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(
		handler.CreateAnswer,
		http.MethodPost,
		"/inquiries/x/answers",
		testInquiryID,
		testInquiryReaderID,
		`{"body":"They follow the river.","language":"eng"}`,
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if service.gotAnswerInquiryID != testInquiryID {
		t.Errorf("answer inquiry = %q, want %q", service.gotAnswerInquiryID, testInquiryID)
	}
	if service.gotAnswerAuthorID != testInquiryReaderID {
		t.Errorf("answer author = %q, want %q (the token's subject)", service.gotAnswerAuthorID, testInquiryReaderID)
	}
	// An answer carries no reactions, because it is a reply (KNOT-ADR-052).
	if strings.Contains(recorder.Body.String(), `"my_reactions"`) {
		t.Errorf("answer body = %s, want no reaction fields", recorder.Body.String())
	}
}

func TestCreateAnswerRequiresAuthentication(t *testing.T) {
	service := &fakeInquiriesService{}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.CreateAnswer, http.MethodPost, "/inquiries/x/answers", testInquiryID, "", `{"body":"y","language":"eng"}`)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if service.createAnswerCalls != 0 {
		t.Errorf("service calls = %d, want 0", service.createAnswerCalls)
	}
}

func TestCreateAnswerOnUnknownInquiryIsNotFound(t *testing.T) {
	service := &fakeInquiriesService{createAnswerErr: inquiries.ErrNotFound}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.CreateAnswer, http.MethodPost, "/inquiries/x/answers", testInquiryID, testInquiryReaderID, `{"body":"y","language":"eng"}`)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestListAnswersUsesTheWiderAnswerLimit(t *testing.T) {
	service := &fakeInquiriesService{
		listAnswersResult: []inquiries.Answer{{ID: testInquiryAnswerID, InquiryID: testInquiryID, AuthorID: testAnswerAuthorID}},
	}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.ListAnswers, http.MethodGet, "/inquiries/x/answers", testInquiryID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotLimit != inquiries.DefaultAnswerListLimit {
		t.Errorf("limit = %d, want %d", service.gotLimit, inquiries.DefaultAnswerListLimit)
	}
	if !strings.Contains(recorder.Body.String(), `"answers":[`) {
		t.Errorf("body = %s, want an answers array", recorder.Body.String())
	}
}

func TestListAnswersOnUnknownInquiryIsNotFound(t *testing.T) {
	service := &fakeInquiriesService{listAnswersErr: inquiries.ErrNotFound}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.ListAnswers, http.MethodGet, "/inquiries/x/answers", testInquiryID, "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGetAnswerEnrichesItsAuthor(t *testing.T) {
	answer := inquiries.Answer{ID: testInquiryAnswerID, InquiryID: testInquiryID, AuthorID: testAnswerAuthorID, Body: "They follow the river."}
	service := &fakeInquiriesService{getAnswerResult: answer}
	authors := &fakeAuthorService{users: map[string]*identity.User{
		testAnswerAuthorID: {ID: testAnswerAuthorID, DisplayName: "Grace Hopper"},
	}}
	handler := newInquiriesHandler(t, service, authors, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.GetAnswer, http.MethodGet, "/answers/x", testInquiryAnswerID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"author_display_name":"Grace Hopper"`) {
		t.Errorf("body = %s, want the answerer named", recorder.Body.String())
	}
}

func TestGetAnswerUnknownIsNotFound(t *testing.T) {
	service := &fakeInquiriesService{getAnswerErr: inquiries.ErrNotFound}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.GetAnswer, http.MethodGet, "/answers/x", testInquiryAnswerID, "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGetAnswerUnrecognisedErrorIsInternal(t *testing.T) {
	service := &fakeInquiriesService{getAnswerErr: errors.New("database is on fire")}
	handler := newInquiriesHandler(t, service, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{})

	recorder := doInquiryRequest(handler.GetAnswer, http.MethodGet, "/answers/x", testInquiryAnswerID, "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "on fire") {
		t.Errorf("body = %s, want no internal detail", recorder.Body.String())
	}
}

func TestNewInquiriesHandlerRejectsMissingDependencies(t *testing.T) {
	logger := discardLogger()

	if _, err := NewInquiriesHandler(nil, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger); err == nil {
		t.Error("NewInquiriesHandler(nil service) error = nil, want an error")
	}
	if _, err := NewInquiriesHandler(&fakeInquiriesService{}, nil, &fakeRootedService{}, &fakeReactionsLookup{}, logger); err == nil {
		t.Error("NewInquiriesHandler(nil authors) error = nil, want an error")
	}
	if _, err := NewInquiriesHandler(&fakeInquiriesService{}, &fakeAuthorService{}, nil, &fakeReactionsLookup{}, logger); err == nil {
		t.Error("NewInquiriesHandler(nil rooted) error = nil, want an error")
	}
	if _, err := NewInquiriesHandler(&fakeInquiriesService{}, &fakeAuthorService{}, &fakeRootedService{}, nil, logger); err == nil {
		t.Error("NewInquiriesHandler(nil reactions) error = nil, want an error")
	}
	if _, err := NewInquiriesHandler(&fakeInquiriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, nil); err == nil {
		t.Error("NewInquiriesHandler(nil logger) error = nil, want an error")
	}
}

// TestInquiryRoutesAreRegistered proves the six routes exist in the real route
// table and are not shadowed by one another. Go's ServeMux refuses conflicting
// patterns at registration, so a mistake here (for example moving a single answer
// under /inquiries/answers/{id}, which would collide with /inquiries/{id}/answers)
// would panic rather than reach this assertion.
func TestInquiryRoutesAreRegistered(t *testing.T) {
	handler, err := newTestRouter(t, discardLogger(), &AuthHandler{})
	if err != nil {
		t.Fatalf("newTestRouter() error = %v, want nil", err)
	}

	cases := []struct {
		method string
		path   string
		want   int
	}{
		// No credential on a protected route is 401.
		{http.MethodPost, "/inquiries", http.StatusUnauthorized},
		{http.MethodPost, "/inquiries/" + testInquiryID + "/answers", http.StatusUnauthorized},
		// The public reads reach the handler, which answers from the stub service.
		{http.MethodGet, "/inquiries", http.StatusOK},
		{http.MethodGet, "/inquiries/" + testInquiryID, http.StatusOK},
		{http.MethodGet, "/inquiries/" + testInquiryID + "/answers", http.StatusOK},
		{http.MethodGet, "/answers/" + testInquiryAnswerID, http.StatusOK},
		{http.MethodGet, "/inquiries/" + testInquiryID + "/reactions", http.StatusOK},
	}

	for _, testCase := range cases {
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != testCase.want {
			t.Errorf("%s %s status = %d, want %d", testCase.method, testCase.path, recorder.Code, testCase.want)
		}
	}
}
