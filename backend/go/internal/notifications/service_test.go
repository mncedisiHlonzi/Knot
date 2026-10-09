package notifications

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// Canonical UUID text used throughout the notification tests.
const (
	testUserID   = "11111111-1111-4111-8111-111111111111"
	testActorID  = "22222222-2222-4222-8222-222222222222"
	testEntityID = "33333333-3333-4333-8333-333333333333"
	testID       = "44444444-4444-4444-8444-444444444444"
	testOtherID  = "55555555-5555-4555-8555-555555555555"
)

// testNow is the fixed clock the fixtures are stamped with.
var testNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// fakeStore records the calls it received and returns canned results, so the
// business rules can be tested without a database.
type fakeStore struct {
	createResult Notification
	createErr    error
	listResult   []Notification
	listNext     *Cursor
	listErr      error
	countResult  int
	countErr     error
	markReadErr  error
	markAllCount int
	markAllErr   error

	gotCreate      Notification
	gotListUserID  string
	gotListCursor  *Cursor
	gotListLimit   int
	gotCountID     string
	gotMarkReadID  string
	gotMarkReadFor string
	gotMarkAllFor  string

	createCalls   int
	listCalls     int
	countCalls    int
	markReadCalls int
	markAllCalls  int
}

func (f *fakeStore) Create(_ context.Context, notification Notification) (Notification, error) {
	f.createCalls++
	f.gotCreate = notification
	if f.createErr != nil {
		return Notification{}, f.createErr
	}

	result := f.createResult
	if result.ID == "" {
		// Emulate the database generating identity and a timestamp.
		result = notification
		result.ID = testID
		result.CreatedAt = testNow
	}

	return result, nil
}

func (f *fakeStore) List(_ context.Context, userID string, cursor *Cursor, limit int) ([]Notification, *Cursor, error) {
	f.listCalls++
	f.gotListUserID = userID
	f.gotListCursor = cursor
	f.gotListLimit = limit
	return f.listResult, f.listNext, f.listErr
}

func (f *fakeStore) UnreadCount(_ context.Context, userID string) (int, error) {
	f.countCalls++
	f.gotCountID = userID
	return f.countResult, f.countErr
}

func (f *fakeStore) MarkRead(_ context.Context, id, userID string) error {
	f.markReadCalls++
	f.gotMarkReadID = id
	f.gotMarkReadFor = userID
	return f.markReadErr
}

func (f *fakeStore) MarkAllRead(_ context.Context, userID string) (int, error) {
	f.markAllCalls++
	f.gotMarkAllFor = userID
	return f.markAllCount, f.markAllErr
}

// fakeLogger records the warnings the service emitted, so a test can prove a
// non-critical failure was reported rather than swallowed silently.
type fakeLogger struct {
	warnings []string
}

func (f *fakeLogger) WarnContext(_ context.Context, msg string, args ...any) {
	entry := msg
	for _, arg := range args {
		entry += " " + toText(arg)
	}
	f.warnings = append(f.warnings, entry)
}

func toText(arg any) string {
	switch value := arg.(type) {
	case string:
		return value
	case error:
		return value.Error()
	default:
		return "?"
	}
}

// newTestService returns a service over a fresh fake store and logger.
func newTestService(t *testing.T) (*Service, *fakeStore, *fakeLogger) {
	t.Helper()

	store := &fakeStore{}
	logger := &fakeLogger{}

	service, err := NewService(store, logger)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, store, logger
}

// validCreateInput is a minimal input that passes every rule.
func validCreateInput() CreateInput {
	return CreateInput{
		UserID:     testUserID,
		ActorID:    testActorID,
		EventType:  EventVersionCreated,
		EntityType: EntityVersion,
		EntityID:   testEntityID,
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeLogger{}); err == nil {
		t.Error("NewService(nil store) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, nil); err == nil {
		t.Error("NewService(nil logger) error = nil, want an error")
	}
}

func TestCreateHappyPath(t *testing.T) {
	service, store, _ := newTestService(t)

	created, err := service.Create(context.Background(), validCreateInput())
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if store.createCalls != 1 {
		t.Fatalf("store create calls = %d, want 1", store.createCalls)
	}
	if store.gotCreate.UserID != testUserID {
		t.Errorf("stored user id = %q, want %q", store.gotCreate.UserID, testUserID)
	}
	if store.gotCreate.ActorID != testActorID {
		t.Errorf("stored actor id = %q, want %q", store.gotCreate.ActorID, testActorID)
	}
	if store.gotCreate.EventType != EventVersionCreated {
		t.Errorf("stored event type = %q, want %q", store.gotCreate.EventType, EventVersionCreated)
	}
	if store.gotCreate.EntityType != EntityVersion {
		t.Errorf("stored entity type = %q, want %q", store.gotCreate.EntityType, EntityVersion)
	}
	if store.gotCreate.EntityID != testEntityID {
		t.Errorf("stored entity id = %q, want %q", store.gotCreate.EntityID, testEntityID)
	}
	if created.ID != testID {
		t.Errorf("created id = %q, want the stored id %q", created.ID, testID)
	}
}

func TestCreateRefusesSelfNotification(t *testing.T) {
	service, store, _ := newTestService(t)

	input := validCreateInput()
	input.ActorID = input.UserID

	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrSelfNotification) {
		t.Fatalf("Create() error = %v, want ErrSelfNotification", err)
	}
	if store.createCalls != 0 {
		t.Errorf("store create calls = %d, want 0 — a self-notification must never be stored", store.createCalls)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		alter func(in *CreateInput)
	}{
		{"recipient is not a uuid", func(in *CreateInput) { in.UserID = "not-a-uuid" }},
		{"actor is not a uuid", func(in *CreateInput) { in.ActorID = "not-a-uuid" }},
		{"entity id is not a uuid", func(in *CreateInput) { in.EntityID = "not-a-uuid" }},
		{"event is unknown", func(in *CreateInput) { in.EventType = "story.published" }},
		{"entity does not match the event", func(in *CreateInput) { in.EntityType = EntityComment }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, _ := newTestService(t)

			input := validCreateInput()
			test.alter(&input)

			_, err := service.Create(context.Background(), input)
			if err == nil {
				t.Fatal("Create() error = nil, want an error")
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("Create() error = %v, want ErrValidation", err)
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Create() error = %v, want a *ValidationError", err)
			}
			if validation.Field == "" || validation.Message == "" {
				t.Errorf("ValidationError = %+v, want a field and a message", validation)
			}

			if store.createCalls != 0 {
				t.Errorf("store create calls = %d, want 0 — invalid input must not reach the store", store.createCalls)
			}
		})
	}
}

func TestCreateWrapsStoreFailure(t *testing.T) {
	service, store, _ := newTestService(t)
	store.createErr = errors.New("connection reset by peer")

	_, err := service.Create(context.Background(), validCreateInput())
	if err == nil {
		t.Fatal("Create() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "connection reset by peer") {
		t.Errorf("Create() error = %v, want the store error wrapped", err)
	}
}

func TestListHappyPath(t *testing.T) {
	service, store, _ := newTestService(t)
	store.listResult = []Notification{{ID: testID, UserID: testUserID, ActorID: testActorID}}
	store.listNext = &Cursor{}

	page, next, err := service.List(context.Background(), testUserID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(page) != 1 {
		t.Fatalf("len(page) = %d, want 1", len(page))
	}
	if store.gotListUserID != testUserID {
		t.Errorf("store user id = %q, want %q", store.gotListUserID, testUserID)
	}
	if store.gotListCursor != nil {
		t.Errorf("store cursor = %v, want nil for the first page", store.gotListCursor)
	}
	if store.gotListLimit != DefaultListLimit {
		t.Errorf("store limit = %d, want %d", store.gotListLimit, DefaultListLimit)
	}
	if next == "" {
		t.Error("next cursor is empty, want the encoded cursor the store returned")
	}
}

func TestListAcceptsACursor(t *testing.T) {
	service, store, _ := newTestService(t)
	store.listResult = []Notification{}

	raw := NewCursor(testNow, testID).Encode()

	if _, _, err := service.List(context.Background(), testUserID, raw, DefaultListLimit); err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if store.gotListCursor == nil {
		t.Fatal("store cursor = nil, want the decoded cursor")
	}
	if store.gotListCursor.ID() != testID {
		t.Errorf("store cursor id = %q, want %q", store.gotListCursor.ID(), testID)
	}
	if !store.gotListCursor.CreatedAt().Equal(testNow) {
		t.Errorf("store cursor created at = %s, want %s", store.gotListCursor.CreatedAt(), testNow)
	}
}

func TestListReturnsAnEmptyArrayNotNil(t *testing.T) {
	service, store, _ := newTestService(t)
	store.listResult = nil

	page, next, err := service.List(context.Background(), testUserID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if page == nil {
		t.Fatal("page = nil, want an empty slice so the response carries [] rather than null")
	}
	if len(page) != 0 {
		t.Errorf("len(page) = %d, want 0", len(page))
	}
	if next != "" {
		t.Errorf("next cursor = %q, want empty on the last page", next)
	}
}

func TestListRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		cursor  string
		limit   int
		wantErr error
	}{
		{name: "user is not a uuid", userID: "not-a-uuid", limit: DefaultListLimit, wantErr: ErrNotFound},
		{name: "limit is zero", userID: testUserID, limit: 0, wantErr: ErrValidation},
		{name: "limit is negative", userID: testUserID, limit: -1, wantErr: ErrValidation},
		{name: "limit is above the maximum", userID: testUserID, limit: MaxListLimit + 1, wantErr: ErrValidation},
		{name: "cursor is not base64", userID: testUserID, cursor: "!!!not-base64!!!", limit: DefaultListLimit, wantErr: ErrValidation},
		{name: "cursor has no separator", userID: testUserID, cursor: encodeCursorPayload("2026-10-09T12:00:00Z"), limit: DefaultListLimit, wantErr: ErrValidation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, _ := newTestService(t)

			_, _, err := service.List(context.Background(), test.userID, test.cursor, test.limit)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("List() error = %v, want %v", err, test.wantErr)
			}
			if store.listCalls != 0 {
				t.Errorf("store list calls = %d, want 0 — rejected input must not reach the store", store.listCalls)
			}
		})
	}
}

func TestUnreadCount(t *testing.T) {
	service, store, _ := newTestService(t)
	store.countResult = 7

	count, err := service.UnreadCount(context.Background(), testUserID)
	if err != nil {
		t.Fatalf("UnreadCount() error = %v, want nil", err)
	}
	if count != 7 {
		t.Errorf("count = %d, want 7", count)
	}
	if store.gotCountID != testUserID {
		t.Errorf("store user id = %q, want %q", store.gotCountID, testUserID)
	}
}

func TestUnreadCountRejectsMalformedUserID(t *testing.T) {
	service, store, _ := newTestService(t)

	if _, err := service.UnreadCount(context.Background(), "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UnreadCount() error = %v, want ErrNotFound", err)
	}
	if store.countCalls != 0 {
		t.Errorf("store count calls = %d, want 0", store.countCalls)
	}
}

func TestMarkRead(t *testing.T) {
	service, store, _ := newTestService(t)

	if err := service.MarkRead(context.Background(), testID, testUserID); err != nil {
		t.Fatalf("MarkRead() error = %v, want nil", err)
	}
	if store.gotMarkReadID != testID {
		t.Errorf("store id = %q, want %q", store.gotMarkReadID, testID)
	}
	if store.gotMarkReadFor != testUserID {
		t.Errorf("store user id = %q, want %q — a notification is marked read for its owner only", store.gotMarkReadFor, testUserID)
	}
}

func TestMarkReadMapsNotFound(t *testing.T) {
	service, store, _ := newTestService(t)
	store.markReadErr = ErrNotFound

	if err := service.MarkRead(context.Background(), testID, testUserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkRead() error = %v, want ErrNotFound", err)
	}
}

func TestMarkReadRejectsMalformedIDs(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		userID string
	}{
		{name: "notification id", id: "not-a-uuid", userID: testUserID},
		{name: "user id", id: testID, userID: "not-a-uuid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, _ := newTestService(t)

			if err := service.MarkRead(context.Background(), test.id, test.userID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("MarkRead() error = %v, want ErrNotFound", err)
			}
			if store.markReadCalls != 0 {
				t.Errorf("store mark read calls = %d, want 0", store.markReadCalls)
			}
		})
	}
}

func TestMarkAllRead(t *testing.T) {
	service, store, _ := newTestService(t)
	store.markAllCount = 3

	updated, err := service.MarkAllRead(context.Background(), testUserID)
	if err != nil {
		t.Fatalf("MarkAllRead() error = %v, want nil", err)
	}
	if updated != 3 {
		t.Errorf("updated = %d, want 3", updated)
	}
	if store.gotMarkAllFor != testUserID {
		t.Errorf("store user id = %q, want %q", store.gotMarkAllFor, testUserID)
	}
}

func TestMarkAllReadRejectsMalformedUserID(t *testing.T) {
	service, store, _ := newTestService(t)

	if _, err := service.MarkAllRead(context.Background(), "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkAllRead() error = %v, want ErrNotFound", err)
	}
	if store.markAllCalls != 0 {
		t.Errorf("store mark all calls = %d, want 0", store.markAllCalls)
	}
}

// encodeCursorPayload base64url-encodes an arbitrary payload, so a test can build
// a syntactically valid but structurally wrong cursor.
func encodeCursorPayload(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// ---------------------------------------------------------------------------
// The Tell My People hooks
// ---------------------------------------------------------------------------

func TestNotifyVersionCreated(t *testing.T) {
	service, store, _ := newTestService(t)

	if err := service.NotifyVersionCreated(context.Background(), testUserID, testActorID, testEntityID); err != nil {
		t.Fatalf("NotifyVersionCreated() error = %v, want nil", err)
	}

	if store.gotCreate.EventType != EventVersionCreated {
		t.Errorf("event type = %q, want %q", store.gotCreate.EventType, EventVersionCreated)
	}
	if store.gotCreate.EntityType != EntityVersion {
		t.Errorf("entity type = %q, want %q", store.gotCreate.EntityType, EntityVersion)
	}
	if store.gotCreate.UserID != testUserID || store.gotCreate.ActorID != testActorID {
		t.Errorf("recipient/actor = %q/%q, want %q/%q", store.gotCreate.UserID, store.gotCreate.ActorID, testUserID, testActorID)
	}
	if store.gotCreate.EntityID != testEntityID {
		t.Errorf("entity id = %q, want %q", store.gotCreate.EntityID, testEntityID)
	}
}

func TestNotifyCommentCreated(t *testing.T) {
	service, store, _ := newTestService(t)

	if err := service.NotifyCommentCreated(context.Background(), testUserID, testActorID, testEntityID); err != nil {
		t.Fatalf("NotifyCommentCreated() error = %v, want nil", err)
	}

	if store.gotCreate.EventType != EventCommentCreated {
		t.Errorf("event type = %q, want %q", store.gotCreate.EventType, EventCommentCreated)
	}
	if store.gotCreate.EntityType != EntityComment {
		t.Errorf("entity type = %q, want %q", store.gotCreate.EntityType, EntityComment)
	}
}

func TestNotifyBridgeCreated(t *testing.T) {
	service, store, _ := newTestService(t)

	if err := service.NotifyBridgeCreated(context.Background(), testUserID, testActorID, testEntityID); err != nil {
		t.Fatalf("NotifyBridgeCreated() error = %v, want nil", err)
	}

	if store.gotCreate.EventType != EventBridgeCreated {
		t.Errorf("event type = %q, want %q", store.gotCreate.EventType, EventBridgeCreated)
	}
	if store.gotCreate.EntityType != EntityBridge {
		t.Errorf("entity type = %q, want %q", store.gotCreate.EntityType, EntityBridge)
	}
}

func TestNotifySelfIsNotLogged(t *testing.T) {
	service, store, logger := newTestService(t)

	// The hook is called with the actor as the recipient.
	err := service.NotifyVersionCreated(context.Background(), testUserID, testUserID, testEntityID)
	if !errors.Is(err, ErrSelfNotification) {
		t.Fatalf("NotifyVersionCreated() error = %v, want ErrSelfNotification", err)
	}
	if store.createCalls != 0 {
		t.Errorf("store create calls = %d, want 0", store.createCalls)
	}
	if len(logger.warnings) != 0 {
		t.Errorf("warnings = %v, want none — acting on your own content is ordinary", logger.warnings)
	}
}

func TestNotifyLogsAndReturnsStoreFailure(t *testing.T) {
	service, store, logger := newTestService(t)
	store.createErr = errors.New("connection reset by peer")

	err := service.NotifyCommentCreated(context.Background(), testUserID, testActorID, testEntityID)
	if err == nil {
		t.Fatal("NotifyCommentCreated() error = nil, want the store failure")
	}
	if len(logger.warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", logger.warnings)
	}
	if !strings.Contains(logger.warnings[0], "could not record notification") {
		t.Errorf("warning = %q, want it to name the failed write", logger.warnings[0])
	}
	if !strings.Contains(logger.warnings[0], "connection reset by peer") {
		t.Errorf("warning = %q, want it to carry the cause", logger.warnings[0])
	}
}

func TestNotifyRejectsMalformedIDs(t *testing.T) {
	service, store, _ := newTestService(t)

	// A caller in another domain can only hand over ids it holds, but a malformed
	// one is still refused rather than stored.
	if err := service.NotifyBridgeCreated(context.Background(), "not-a-uuid", testActorID, testEntityID); err == nil {
		t.Fatal("NotifyBridgeCreated() error = nil, want a validation error")
	}
	if store.createCalls != 0 {
		t.Errorf("store create calls = %d, want 0", store.createCalls)
	}
}
