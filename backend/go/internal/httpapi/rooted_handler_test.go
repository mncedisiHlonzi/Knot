package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/rooted"
)

// testRootedAt is the fixed timestamp the rooted fixtures are stamped with.
var testRootedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// fakeRootedService is an in-memory RootedService that also serves as the
// enrichment lookup the content handlers use. It records the input it received and
// returns canned results, so handler behaviour can be tested without a database.
type fakeRootedService struct {
	setResult rooted.Signal
	setErr    error

	mineResult []rooted.Signal
	mineErr    error

	publicResult []rooted.Signal
	publicErr    error

	batchResult map[string]*rooted.Signal
	batchErr    error

	gotSet       rooted.SetSignalInput
	gotSetUserID string
	setCalls     int
	mineCalls    int
	publicCalls  int
	batchCalls   int
	gotBatchIDs  []string
}

func (f *fakeRootedService) SetSignal(_ context.Context, userID string, in rooted.SetSignalInput) (rooted.Signal, error) {
	f.setCalls++
	f.gotSet = in
	f.gotSetUserID = userID
	return f.setResult, f.setErr
}

func (f *fakeRootedService) GetMySignals(_ context.Context, _ string) ([]rooted.Signal, error) {
	f.mineCalls++
	return f.mineResult, f.mineErr
}

func (f *fakeRootedService) GetPublicSignals(_ context.Context, _ string) ([]rooted.Signal, error) {
	f.publicCalls++
	return f.publicResult, f.publicErr
}

func (f *fakeRootedService) BatchGetPrimaryPublicSignals(_ context.Context, userIDs []string) (map[string]*rooted.Signal, error) {
	f.batchCalls++
	f.gotBatchIDs = append(f.gotBatchIDs, userIDs...)
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	if f.batchResult == nil {
		return map[string]*rooted.Signal{}, nil
	}
	return f.batchResult, nil
}

// rootedSignal is a well-formed stored signal for testUserID.
func rootedSignal() rooted.Signal {
	return rooted.Signal{
		ID:             "99999999-9999-4999-8999-999999999999",
		UserID:         testUserID,
		Place:          "Cape Town",
		DurationBucket: rooted.DurationLifelong,
		IsPublic:       true,
		IsPrimary:      true,
		CreatedAt:      testRootedAt,
		UpdatedAt:      testRootedAt,
	}
}

// newRouterWithRooted assembles the full router the way cmd/knot does, with both
// the Rooted routes and the content-enrichment lookup backed by rootedService, so
// the tests exercise the real middleware chain and route table.
func newRouterWithRooted(t *testing.T, logger *slog.Logger, rootedService RootedService, storiesService StoriesService, versionsService VersionsService, conversationsService ConversationsService) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(storiesService, rootedService, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(versionsService, rootedService, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(conversationsService, rootedService, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(rootedService, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler, err := NewDiscoveryHandler(fakeDiscoveryService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// newRootedRouter is newRouterWithRooted with the other handlers left as inert
// fakes, for the Rooted endpoint tests.
func newRootedRouter(t *testing.T, rootedService RootedService) http.Handler {
	t.Helper()
	return newRouterWithRooted(
		t,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		rootedService,
		&fakeStoriesService{},
		&fakeVersionsService{},
		&fakeConversationsService{},
	)
}

func validRootedBody() string {
	return `{"place": "Cape Town", "duration_bucket": "lifelong", "is_public": true}`
}

func TestSetSignalHappyPath(t *testing.T) {
	service := &fakeRootedService{setResult: rootedSignal()}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted", validRootedBody(), testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body signalEnvelope
	decodeBody(t, recorder, &body)

	if body.Signal.Place != "Cape Town" {
		t.Errorf("place = %q, want %q", body.Signal.Place, "Cape Town")
	}
	if body.Signal.DurationBucket != rooted.DurationLifelong {
		t.Errorf("duration_bucket = %q, want %q", body.Signal.DurationBucket, rooted.DurationLifelong)
	}
	if !body.Signal.IsPrimary || !body.Signal.IsPublic {
		t.Errorf("flags = (primary %v, public %v), want both true", body.Signal.IsPrimary, body.Signal.IsPublic)
	}
	if body.Signal.UserID != testUserID {
		t.Errorf("user_id = %q, want the authenticated user %q", body.Signal.UserID, testUserID)
	}
	if service.gotSetUserID != testUserID {
		t.Errorf("service received user id %q, want %q", service.gotSetUserID, testUserID)
	}
	if service.gotSet.Place != "Cape Town" || service.gotSet.DurationBucket != rooted.DurationLifelong {
		t.Errorf("service received %+v, want the request values", service.gotSet)
	}
}

func TestSetSignalDefaultsToPublicWhenOmitted(t *testing.T) {
	service := &fakeRootedService{setResult: rootedSignal()}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted",
		`{"place": "Cape Town", "duration_bucket": "lifelong"}`, testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !service.gotSet.IsPublic {
		t.Error("is_public = false, want the default public signal when the field is omitted")
	}
}

func TestSetSignalRespectsExplicitPrivacy(t *testing.T) {
	service := &fakeRootedService{setResult: rootedSignal()}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted",
		`{"place": "Cape Town", "duration_bucket": "lifelong", "is_public": false}`, testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotSet.IsPublic {
		t.Error("is_public = true, want the explicit false to be passed through")
	}
}

func TestSetSignalRequiresAuthentication(t *testing.T) {
	service := &fakeRootedService{setResult: rootedSignal()}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted", validRootedBody(), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}
	if service.setCalls != 0 {
		t.Errorf("service received %d calls, want 0 — an unauthenticated request must not write", service.setCalls)
	}
}

func TestSetSignalValidationFailure(t *testing.T) {
	service := &fakeRootedService{
		setErr: &rooted.ValidationError{Field: "place", Message: "is required"},
	}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted",
		`{"place": "", "duration_bucket": "lifelong"}`, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestSetSignalRejectsUnknownField(t *testing.T) {
	handler := newRootedRouter(t, &fakeRootedService{setResult: rootedSignal()})

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted",
		`{"place": "Cape Town", "duration_bucket": "lifelong", "score": 10}`, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestGetMySignalsHappyPath(t *testing.T) {
	private := rootedSignal()
	private.IsPublic = false
	service := &fakeRootedService{mineResult: []rooted.Signal{rootedSignal(), private}}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/users/me/rooted", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body signalListResponse
	decodeBody(t, recorder, &body)

	if len(body.Signals) != 2 {
		t.Fatalf("signals = %d, want 2 (the owner sees private signals too)", len(body.Signals))
	}
	if service.mineCalls != 1 {
		t.Errorf("service received %d calls, want 1", service.mineCalls)
	}
}

func TestGetMySignalsRequiresAuthentication(t *testing.T) {
	service := &fakeRootedService{}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/users/me/rooted", "", "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if service.mineCalls != 0 {
		t.Errorf("service received %d calls, want 0", service.mineCalls)
	}
}

func TestGetMySignalsEmitsEmptyArrayNotNull(t *testing.T) {
	handler := newRootedRouter(t, &fakeRootedService{mineResult: nil})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/me/rooted", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"signals":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", recorder.Body.String())
	}
}

func TestGetUserSignalsHappyPath(t *testing.T) {
	service := &fakeRootedService{publicResult: []rooted.Signal{rootedSignal()}}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/rooted", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body signalListResponse
	decodeBody(t, recorder, &body)

	// The service filters to public signals; the handler returns exactly what it
	// was given.
	if len(body.Signals) != 1 || body.Signals[0].Place != "Cape Town" {
		t.Errorf("signals = %+v, want the single public signal", body.Signals)
	}
	if service.publicCalls != 1 {
		t.Errorf("service received %d calls, want 1", service.publicCalls)
	}
}

func TestGetUserSignalsIsPublicFiltered(t *testing.T) {
	// The service is the layer that hides private signals; this test locks in that
	// the handler never adds more than it was given.
	service := &fakeRootedService{publicResult: nil}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/rooted", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"signals":[]`) {
		t.Errorf("body = %s, want an empty array for a user with no public signal", recorder.Body.String())
	}
}

func TestGetUserSignalsNotFound(t *testing.T) {
	service := &fakeRootedService{publicErr: rooted.ErrUserNotFound}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+"00000000-0000-4000-8000-000000000000"+"/rooted", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestRootedUnexpectedFailureIsInternalError(t *testing.T) {
	service := &fakeRootedService{setErr: context.DeadlineExceeded}
	handler := newRootedRouter(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/users/me/rooted", validRootedBody(), testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestNewRootedHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewRootedHandler(nil, logger); err == nil {
		t.Error("NewRootedHandler(nil, logger) error = nil, want an error")
	}
	if _, err := NewRootedHandler(&fakeRootedService{}, nil); err == nil {
		t.Error("NewRootedHandler(service, nil) error = nil, want an error")
	}
}
