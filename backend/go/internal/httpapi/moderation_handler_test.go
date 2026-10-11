package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/moderation"
)

// fakeModerationService is a scriptable ModerationService for handler tests.
type fakeModerationService struct {
	blockCalls int
	gotBlocker string
	gotBlocked string
	blockErr   error

	unblockCalls int
	unblockErr   error

	listBlocksResult []moderation.Block
	listBlocksNext   string
	listBlocksErr    error

	reportResult   moderation.Report
	reportErr      error
	gotReportInput moderation.CreateReportInput

	myReports     []moderation.Report
	myReportsNext string
	myReportsErr  error
}

func (f *fakeModerationService) Block(_ context.Context, blockerID, blockedID string) error {
	f.blockCalls++
	f.gotBlocker = blockerID
	f.gotBlocked = blockedID
	return f.blockErr
}

func (f *fakeModerationService) Unblock(_ context.Context, _, _ string) error {
	f.unblockCalls++
	return f.unblockErr
}

func (f *fakeModerationService) ListBlocks(_ context.Context, _, _ string, _ int) ([]moderation.Block, string, error) {
	return f.listBlocksResult, f.listBlocksNext, f.listBlocksErr
}

func (f *fakeModerationService) CreateReport(_ context.Context, in moderation.CreateReportInput) (moderation.Report, error) {
	f.gotReportInput = in
	if f.reportErr != nil {
		return moderation.Report{}, f.reportErr
	}
	return f.reportResult, nil
}

func (f *fakeModerationService) ListMyReports(_ context.Context, _, _ string, _ int) ([]moderation.Report, string, error) {
	return f.myReports, f.myReportsNext, f.myReportsErr
}

// newTestModerationHandler returns a moderation handler over a default fake, for
// the router-construction tests.
func newTestModerationHandler(t *testing.T, logger *slog.Logger) *ModerationHandler {
	t.Helper()

	handler, err := NewModerationHandler(
		&fakeModerationService{},
		&fakeAuthorService{users: map[string]*identity.User{}},
		logger,
	)
	if err != nil {
		t.Fatalf("NewModerationHandler() error = %v, want nil", err)
	}
	return handler
}

// newModerationHandler builds a handler over the given service and user lookup.
func newModerationHandler(t *testing.T, service ModerationService, users AuthorLookup) *ModerationHandler {
	t.Helper()

	handler, err := NewModerationHandler(service, users, discardLogger())
	if err != nil {
		t.Fatalf("NewModerationHandler() error = %v, want nil", err)
	}
	return handler
}

// doModerationRequest calls a handler method directly, setting the given path
// values and the authenticated user on the context.
func doModerationRequest(handler http.HandlerFunc, method, path, userID, body string, pathValues map[string]string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	var request *http.Request
	if reader != nil {
		request = httptest.NewRequest(method, path, reader)
	} else {
		request = httptest.NewRequest(method, path, nil)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	if userID != "" {
		request = request.WithContext(withUserID(request.Context(), userID))
	}

	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

const (
	testModeratorID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testBlockedID   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testReportID    = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	testEntityID    = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
)

func TestCreateReportRequiresAuthentication(t *testing.T) {
	service := &fakeModerationService{}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(handler.CreateReport, http.MethodPost, "/reports", "", `{}`, nil)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if service.gotReportInput.ReporterID != "" {
		t.Errorf("CreateReport called with reporter %q, want none", service.gotReportInput.ReporterID)
	}
}

func TestCreateReportStoresAndReturnsTheReport(t *testing.T) {
	created := moderation.Report{
		ID:         testReportID,
		ReporterID: testModeratorID,
		EntityType: moderation.EntityStory,
		EntityID:   testEntityID,
		Category:   moderation.CategoryHarassment,
		CreatedAt:  time.Now().UTC(),
	}
	service := &fakeModerationService{reportResult: created}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(
		handler.CreateReport,
		http.MethodPost,
		"/reports",
		testModeratorID,
		`{"entity_type":"story","entity_id":"`+testEntityID+`","category":"harassment"}`,
		nil,
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if service.gotReportInput.ReporterID != testModeratorID {
		t.Errorf("reporter = %q, want %q", service.gotReportInput.ReporterID, testModeratorID)
	}
	if service.gotReportInput.EntityType != moderation.EntityStory {
		t.Errorf("entity_type = %q, want story", service.gotReportInput.EntityType)
	}

	body := recorder.Body.String()
	for _, want := range []string{`"id":"` + testReportID + `"`, `"category":"harassment"`, `"entity_type":"story"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response body %s missing %s", body, want)
		}
	}
}

func TestCreateReportMapsAlreadyReportedToConflict(t *testing.T) {
	service := &fakeModerationService{reportErr: moderation.ErrAlreadyReported}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(
		handler.CreateReport,
		http.MethodPost,
		"/reports",
		testModeratorID,
		`{"entity_type":"story","entity_id":"`+testEntityID+`","category":"spam"}`,
		nil,
	)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestCreateReportMapsEntityNotFound(t *testing.T) {
	service := &fakeModerationService{reportErr: moderation.ErrEntityNotFound}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(
		handler.CreateReport,
		http.MethodPost,
		"/reports",
		testModeratorID,
		`{"entity_type":"story","entity_id":"`+testEntityID+`","category":"spam"}`,
		nil,
	)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestCreateReportMapsValidationToBadRequest(t *testing.T) {
	service := &fakeModerationService{reportErr: &moderation.ValidationError{Field: "category", Message: "is not valid"}}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(
		handler.CreateReport,
		http.MethodPost,
		"/reports",
		testModeratorID,
		`{"entity_type":"story","entity_id":"`+testEntityID+`","category":"nonsense"}`,
		nil,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestListMyReportsReturnsPage(t *testing.T) {
	service := &fakeModerationService{
		myReports: []moderation.Report{{
			ID:         testReportID,
			EntityType: moderation.EntityComment,
			EntityID:   testEntityID,
			Category:   moderation.CategorySpam,
			CreatedAt:  time.Now().UTC(),
		}},
		myReportsNext: "next-token",
	}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(handler.ListMyReports, http.MethodGet, "/reports/mine", testModeratorID, "", nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"reports":[`, `"next_cursor":"next-token"`, `"category":"spam"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response body %s missing %s", body, want)
		}
	}
}

func TestCreateBlockRequiresAuthentication(t *testing.T) {
	service := &fakeModerationService{}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(handler.CreateBlock, http.MethodPost, "/blocks/"+testBlockedID, "", "", map[string]string{"user_id": testBlockedID})

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if service.blockCalls != 0 {
		t.Errorf("Block called %d times, want 0", service.blockCalls)
	}
}

func TestCreateBlockIsNoContent(t *testing.T) {
	service := &fakeModerationService{}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(handler.CreateBlock, http.MethodPost, "/blocks/"+testBlockedID, testModeratorID, "", map[string]string{"user_id": testBlockedID})

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if service.gotBlocker != testModeratorID || service.gotBlocked != testBlockedID {
		t.Errorf("Block(%q, %q), want (%q, %q)", service.gotBlocker, service.gotBlocked, testModeratorID, testBlockedID)
	}
}

func TestCreateBlockMapsSelfBlockToBadRequest(t *testing.T) {
	service := &fakeModerationService{blockErr: moderation.ErrSelfBlock}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(handler.CreateBlock, http.MethodPost, "/blocks/"+testModeratorID, testModeratorID, "", map[string]string{"user_id": testModeratorID})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestDeleteBlockIsNoContent(t *testing.T) {
	service := &fakeModerationService{}
	handler := newModerationHandler(t, service, &fakeAuthorService{})

	recorder := doModerationRequest(handler.DeleteBlock, http.MethodDelete, "/blocks/"+testBlockedID, testModeratorID, "", map[string]string{"user_id": testBlockedID})

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if service.unblockCalls != 1 {
		t.Errorf("Unblock called %d times, want 1", service.unblockCalls)
	}
}

func TestListBlocksEnrichesBlockedUser(t *testing.T) {
	service := &fakeModerationService{
		listBlocksResult: []moderation.Block{{
			ID:        "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
			BlockerID: testModeratorID,
			BlockedID: testBlockedID,
			CreatedAt: time.Now().UTC(),
		}},
	}
	users := &fakeAuthorService{users: map[string]*identity.User{
		testBlockedID: {ID: testBlockedID, DisplayName: "Blocked Person", Role: string(moderation.RoleModerator)},
	}}
	handler := newModerationHandler(t, service, users)

	recorder := doModerationRequest(handler.ListBlocks, http.MethodGet, "/blocks/mine", testModeratorID, "", nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"blocks":[`, `"display_name":"Blocked Person"`, `"role":"moderator"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response body %s missing %s", body, want)
		}
	}
}
