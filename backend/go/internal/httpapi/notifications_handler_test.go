package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/notifications"
	"github.com/knot/backend/internal/rooted"
)

// Canonical UUID text used throughout the notifications handler tests. They are
// distinct from the fixtures other handler tests use so that a mixed-up id is a
// visible failure rather than a coincidence.
const (
	testNotificationID       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testSecondNotificationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testNotificationActorID  = "22222222-2222-4222-8222-222222222222"
	testNotificationEntityID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

// fakeNotificationsService records the calls it received and returns canned
// results, so the handler can be tested without a database.
type fakeNotificationsService struct {
	listResult []notifications.Notification
	listNext   string
	listErr    error

	countResult int
	countErr    error

	markReadErr error

	markAllResult int
	markAllErr    error

	gotListUserID  string
	gotListCursor  string
	gotListLimit   int
	gotMarkReadID  string
	gotMarkReadFor string
	gotMarkAllFor  string

	listCalls     int
	countCalls    int
	markReadCalls int
	markAllCalls  int
}

func (f *fakeNotificationsService) List(_ context.Context, userID, rawCursor string, limit int) ([]notifications.Notification, string, error) {
	f.listCalls++
	f.gotListUserID = userID
	f.gotListCursor = rawCursor
	f.gotListLimit = limit
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	return f.listResult, f.listNext, nil
}

func (f *fakeNotificationsService) UnreadCount(_ context.Context, userID string) (int, error) {
	f.countCalls++
	f.gotListUserID = userID
	return f.countResult, f.countErr
}

func (f *fakeNotificationsService) MarkRead(_ context.Context, id, userID string) error {
	f.markReadCalls++
	f.gotMarkReadID = id
	f.gotMarkReadFor = userID
	return f.markReadErr
}

func (f *fakeNotificationsService) MarkAllRead(_ context.Context, userID string) (int, error) {
	f.markAllCalls++
	f.gotMarkAllFor = userID
	return f.markAllResult, f.markAllErr
}

// fakeAuthorService is the batched user lookup the handler enriches with. It
// records the ids it was asked for, so a test can prove the enrichment is one
// deduplicated call.
type fakeAuthorService struct {
	users  map[string]*identity.User
	err    error
	gotIDs []string
	calls  int
}

func (f *fakeAuthorService) UsersByIDs(_ context.Context, ids []string) (map[string]*identity.User, error) {
	f.calls++
	f.gotIDs = append(f.gotIDs, ids...)
	if f.err != nil {
		return nil, f.err
	}

	out := make(map[string]*identity.User, len(ids))
	for _, id := range ids {
		if user, ok := f.users[id]; ok {
			out[id] = user
		}
	}
	return out, nil
}

// notificationActor is the actor fixture for testNotificationActorID.
func notificationActor() *identity.User {
	return &identity.User{
		ID:          testNotificationActorID,
		DisplayName: "Ada Lovelace",
		AvatarURL:   "avatars/" + testNotificationActorID + "/ada.png",
	}
}

// notificationFixture is one stored notification addressed to testUserID.
func notificationFixture() notifications.Notification {
	return notifications.Notification{
		ID:         testNotificationID,
		UserID:     testUserID,
		ActorID:    testNotificationActorID,
		EventType:  notifications.EventVersionCreated,
		EntityType: notifications.EntityVersion,
		EntityID:   testNotificationEntityID,
		CreatedAt:  testNow,
	}
}

// readNotificationFixture is notificationFixture already marked read.
func readNotificationFixture() notifications.Notification {
	readAt := testNow.Add(time.Minute)
	read := notificationFixture()
	read.ID = testSecondNotificationID
	read.ReadAt = &readAt
	return read
}

// fakeNotifier satisfies the versions.Notifier and conversations.Notifier hooks
// that the real services require. The handler tests run the real services over
// in-memory stores, so they need a notifier; what it records is not asserted
// here (the domain tests assert the hook behaviour).
type fakeNotifier struct {
	calls []fakeNotifierCall
	err   error
}

// fakeNotifierCall is one notification the service asked its notifier to send.
type fakeNotifierCall struct {
	kind        string
	recipientID string
	actorID     string
	entityID    string
}

func (f *fakeNotifier) NotifyVersionCreated(_ context.Context, recipientID, actorID, versionID string) error {
	f.calls = append(f.calls, fakeNotifierCall{
		kind:        string(notifications.EventVersionCreated),
		recipientID: recipientID,
		actorID:     actorID,
		entityID:    versionID,
	})
	return f.err
}

func (f *fakeNotifier) NotifyCommentCreated(_ context.Context, recipientID, actorID, commentID string) error {
	f.calls = append(f.calls, fakeNotifierCall{
		kind:        string(notifications.EventCommentCreated),
		recipientID: recipientID,
		actorID:     actorID,
		entityID:    commentID,
	})
	return f.err
}

func (f *fakeNotifier) NotifyBridgeCreated(_ context.Context, recipientID, actorID, bridgeID string) error {
	f.calls = append(f.calls, fakeNotifierCall{
		kind:        string(notifications.EventBridgeCreated),
		recipientID: recipientID,
		actorID:     actorID,
		entityID:    bridgeID,
	})
	return f.err
}

// newTestNotificationsHandler returns a notifications handler over fresh fakes,
// for the router helpers other handler tests use to assemble the full route table.
func newTestNotificationsHandler(t *testing.T, logger *slog.Logger) *NotificationsHandler {
	t.Helper()

	handler, err := NewNotificationsHandler(&fakeNotificationsService{}, &fakeAuthorService{users: map[string]*identity.User{}}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewNotificationsHandler() error = %v, want nil", err)
	}

	return handler
}

// newNotificationsHandler returns the composed router with a notifications
// handler over the given fakes.
func newNotificationsHandler(t *testing.T, service NotificationsService, users NotificationActors, rootedLookup RootedLookup) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	notificationsHandler, err := NewNotificationsHandler(service, users, rootedLookup, logger)
	if err != nil {
		t.Fatalf("NewNotificationsHandler() error = %v, want nil", err)
	}

	return newNotificationsRouter(t, logger, notificationsHandler)
}

// newNotificationsRouter assembles the full router the way cmd/knot does, so the
// tests exercise the real middleware chain and route table.
func newNotificationsRouter(t *testing.T, logger *slog.Logger, notificationsHandler *NotificationsHandler) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
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

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), notificationsHandler, newTestProfileHandler(t, logger), newTestReactionsRouterHandler(t, logger), newTestInquiriesHandler(t, logger), newTestModerationHandler(t, logger), authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestNewNotificationsHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewNotificationsHandler(nil, &fakeAuthorService{}, &fakeRootedService{}, logger); err == nil {
		t.Error("NewNotificationsHandler(nil service) error = nil, want an error")
	}
	if _, err := NewNotificationsHandler(&fakeNotificationsService{}, nil, &fakeRootedService{}, logger); err == nil {
		t.Error("NewNotificationsHandler(nil users) error = nil, want an error")
	}
	if _, err := NewNotificationsHandler(&fakeNotificationsService{}, &fakeAuthorService{}, nil, logger); err == nil {
		t.Error("NewNotificationsHandler(nil rooted) error = nil, want an error")
	}
	if _, err := NewNotificationsHandler(&fakeNotificationsService{}, &fakeAuthorService{}, &fakeRootedService{}, nil); err == nil {
		t.Error("NewNotificationsHandler(nil logger) error = nil, want an error")
	}
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

func TestNotificationsRequireAuthentication(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/notifications"},
		{http.MethodGet, "/notifications/unread_count"},
		{http.MethodPost, "/notifications/" + testNotificationID + "/read"},
		{http.MethodPost, "/notifications/read_all"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			service := &fakeNotificationsService{}
			handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

			recorder := doStoryRequest(handler, route.method, route.path, "", "")

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
				t.Errorf("error code = %q, want %q", code, codeUnauthorized)
			}
			if service.listCalls+service.countCalls+service.markReadCalls+service.markAllCalls != 0 {
				t.Error("service was called, want no calls — an unauthenticated request must not touch an inbox")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /notifications
// ---------------------------------------------------------------------------

func TestListNotificationsHappyPath(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{notificationFixture()}}
	users := &fakeAuthorService{users: map[string]*identity.User{testNotificationActorID: notificationActor()}}
	rootedLookup := &fakeRootedService{batchResult: map[string]*rooted.Signal{
		testNotificationActorID: {
			ID:             "99999999-9999-4999-8999-999999999999",
			UserID:         testNotificationActorID,
			Place:          "Cape Town",
			DurationBucket: rooted.DurationLifelong,
			IsPublic:       true,
			IsPrimary:      true,
		},
	}}
	handler := newNotificationsHandler(t, service, users, rootedLookup)

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if len(body.Notifications) != 1 {
		t.Fatalf("len(notifications) = %d, want 1", len(body.Notifications))
	}
	item := body.Notifications[0]
	if item.ID != testNotificationID {
		t.Errorf("id = %q, want %q", item.ID, testNotificationID)
	}
	if item.EventType != "version.created" {
		t.Errorf("event type = %q, want %q", item.EventType, "version.created")
	}
	if item.EntityType != "version" {
		t.Errorf("entity type = %q, want %q", item.EntityType, "version")
	}
	if item.EntityID != testNotificationEntityID {
		t.Errorf("entity id = %q, want %q", item.EntityID, testNotificationEntityID)
	}
	if item.Read {
		t.Error("read = true, want false for an unread notification")
	}
	if item.Actor == nil {
		t.Fatal("actor = null, want the resolved actor")
	}
	if item.Actor.ID != testNotificationActorID {
		t.Errorf("actor id = %q, want %q", item.Actor.ID, testNotificationActorID)
	}
	if item.Actor.DisplayName != "Ada Lovelace" {
		t.Errorf("actor display name = %q, want %q", item.Actor.DisplayName, "Ada Lovelace")
	}
	wantAvatar := "/users/" + testNotificationActorID + "/avatar?v=ada.png"
	if item.Actor.AvatarURL != wantAvatar {
		t.Errorf("actor avatar url = %q, want %q", item.Actor.AvatarURL, wantAvatar)
	}
	if item.Actor.AuthorRooted == nil || item.Actor.AuthorRooted.Place != "Cape Town" {
		t.Errorf("actor author_rooted = %+v, want the Cape Town summary", item.Actor.AuthorRooted)
	}
	if body.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty on the last page", body.NextCursor)
	}
	if service.gotListUserID != testUserID {
		t.Errorf("list user id = %q, want the authenticated user %q", service.gotListUserID, testUserID)
	}
	if service.gotListLimit != notifications.DefaultListLimit {
		t.Errorf("limit = %d, want the default %d", service.gotListLimit, notifications.DefaultListLimit)
	}
	if users.calls != 1 {
		t.Errorf("actor lookup calls = %d, want exactly 1 batched call", users.calls)
	}
}

func TestListNotificationsMarksReadNotifications(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{readNotificationFixture()}}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if len(body.Notifications) != 1 {
		t.Fatalf("len(notifications) = %d, want 1", len(body.Notifications))
	}
	if !body.Notifications[0].Read {
		t.Error("read = false, want true for a notification with read_at set")
	}
}

func TestListNotificationsDeduplicatesActorLookups(t *testing.T) {
	second := notificationFixture()
	second.ID = testSecondNotificationID

	service := &fakeNotificationsService{listResult: []notifications.Notification{notificationFixture(), second}}
	users := &fakeAuthorService{users: map[string]*identity.User{testNotificationActorID: notificationActor()}}
	handler := newNotificationsHandler(t, service, users, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if len(body.Notifications) != 2 {
		t.Fatalf("len(notifications) = %d, want 2", len(body.Notifications))
	}
	if users.calls != 1 {
		t.Errorf("actor lookup calls = %d, want exactly 1 batched call", users.calls)
	}
	if len(users.gotIDs) != 1 {
		t.Errorf("actor lookup ids = %v, want the one distinct actor", users.gotIDs)
	}
}

func TestListNotificationsEmptyInboxIsAnArray(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{}}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"notifications":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", got)
	}
}

func TestListNotificationsPaginates(t *testing.T) {
	service := &fakeNotificationsService{
		listResult: []notifications.Notification{notificationFixture()},
		listNext:   "next-page-cursor",
	}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications?limit=5&cursor=abc", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if service.gotListLimit != 5 {
		t.Errorf("limit = %d, want 5", service.gotListLimit)
	}
	if service.gotListCursor != "abc" {
		t.Errorf("cursor = %q, want %q", service.gotListCursor, "abc")
	}
	if body.NextCursor != "next-page-cursor" {
		t.Errorf("next_cursor = %q, want %q", body.NextCursor, "next-page-cursor")
	}
}

func TestListNotificationsClampsLimit(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{}}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications?limit=500", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.gotListLimit != notifications.MaxListLimit {
		t.Errorf("limit = %d, want the clamped maximum %d", service.gotListLimit, notifications.MaxListLimit)
	}
}

func TestListNotificationsRejectsBadLimit(t *testing.T) {
	for _, raw := range []string{"0", "-1", "abc"} {
		t.Run(raw, func(t *testing.T) {
			service := &fakeNotificationsService{listResult: []notifications.Notification{}}
			handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

			recorder := doStoryRequest(handler, http.MethodGet, "/notifications?limit="+raw, "", testAccessToken)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
			if service.listCalls != 0 {
				t.Error("service was called, want no calls — a malformed limit must not reach the inbox")
			}
		})
	}
}

func TestListNotificationsUnknownActorIsNull(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{notificationFixture()}}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if body.Notifications[0].Actor != nil {
		t.Errorf("actor = %+v, want null for an unresolvable actor", body.Notifications[0].Actor)
	}
}

func TestListNotificationsSurvivesActorLookupFailure(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{notificationFixture()}}
	users := &fakeAuthorService{err: errors.New("database is down")}
	handler := newNotificationsHandler(t, service, users, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — enrichment is supplementary (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if len(body.Notifications) != 1 {
		t.Fatalf("len(notifications) = %d, want the page even without actor names", len(body.Notifications))
	}
	if body.Notifications[0].Actor != nil {
		t.Errorf("actor = %+v, want null when the lookup failed", body.Notifications[0].Actor)
	}
}

func TestListNotificationsSurvivesRootedFailure(t *testing.T) {
	service := &fakeNotificationsService{listResult: []notifications.Notification{notificationFixture()}}
	users := &fakeAuthorService{users: map[string]*identity.User{testNotificationActorID: notificationActor()}}
	rootedLookup := &fakeRootedService{batchErr: errors.New("rooted is down")}
	handler := newNotificationsHandler(t, service, users, rootedLookup)

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — enrichment is supplementary (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body listNotificationsResponse
	decodeBody(t, recorder, &body)

	if body.Notifications[0].Actor == nil {
		t.Fatal("actor = null, want the actor even when the Rooted summary failed")
	}
	if body.Notifications[0].Actor.AuthorRooted != nil {
		t.Errorf("author_rooted = %+v, want null when the Rooted lookup failed", body.Notifications[0].Actor.AuthorRooted)
	}
}

func TestListNotificationsServiceFailureIsGeneric(t *testing.T) {
	service := &fakeNotificationsService{listErr: errors.New("connection reset by peer")}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications", "", testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
	if got := recorder.Body.String(); strings.Contains(got, "connection reset") {
		t.Errorf("body = %s, want the internal error hidden from the client", got)
	}
}

// ---------------------------------------------------------------------------
// GET /notifications/unread_count
// ---------------------------------------------------------------------------

func TestUnreadCount(t *testing.T) {
	service := &fakeNotificationsService{countResult: 3}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications/unread_count", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body unreadCountResponse
	decodeBody(t, recorder, &body)

	if body.Count != 3 {
		t.Errorf("count = %d, want 3", body.Count)
	}
	if service.gotListUserID != testUserID {
		t.Errorf("user id = %q, want the authenticated user %q", service.gotListUserID, testUserID)
	}
}

func TestUnreadCountServiceFailureIsGeneric(t *testing.T) {
	service := &fakeNotificationsService{countErr: errors.New("connection reset by peer")}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/notifications/unread_count", "", testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// POST /notifications/{id}/read
// ---------------------------------------------------------------------------

func TestMarkRead(t *testing.T) {
	service := &fakeNotificationsService{}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodPost, "/notifications/"+testNotificationID+"/read", "", testAccessToken)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want an empty body for 204", recorder.Body.String())
	}
	if service.gotMarkReadID != testNotificationID {
		t.Errorf("id = %q, want %q", service.gotMarkReadID, testNotificationID)
	}
	if service.gotMarkReadFor != testUserID {
		t.Errorf("user id = %q, want the authenticated user %q — a client must not mark another user's notification", service.gotMarkReadFor, testUserID)
	}
}

func TestMarkReadMissingNotification(t *testing.T) {
	service := &fakeNotificationsService{markReadErr: notifications.ErrNotFound}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodPost, "/notifications/"+testNotificationID+"/read", "", testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestMarkReadValidatesTheId(t *testing.T) {
	service := &fakeNotificationsService{markReadErr: notifications.ErrNotFound}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodPost, "/notifications/not-a-uuid/read", "", testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d — a malformed id names nothing (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestMarkReadServiceFailureIsGeneric(t *testing.T) {
	service := &fakeNotificationsService{markReadErr: errors.New("connection reset by peer")}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodPost, "/notifications/"+testNotificationID+"/read", "", testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// POST /notifications/read_all
// ---------------------------------------------------------------------------

func TestMarkAllRead(t *testing.T) {
	service := &fakeNotificationsService{markAllResult: 4}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodPost, "/notifications/read_all", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body markAllReadResponse
	decodeBody(t, recorder, &body)

	if body.Updated != 4 {
		t.Errorf("updated = %d, want 4", body.Updated)
	}
	if service.gotMarkAllFor != testUserID {
		t.Errorf("user id = %q, want the authenticated user %q", service.gotMarkAllFor, testUserID)
	}
}

func TestMarkAllReadServiceFailureIsGeneric(t *testing.T) {
	service := &fakeNotificationsService{markAllErr: errors.New("connection reset by peer")}
	handler := newNotificationsHandler(t, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodPost, "/notifications/read_all", "", testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
