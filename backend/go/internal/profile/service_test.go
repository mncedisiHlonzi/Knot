package profile

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
)

// Canonical UUID text used throughout the service tests.
const (
	testUserID     = "11111111-1111-4111-8111-111111111111"
	testOtherID    = "22222222-2222-4222-8222-222222222222"
	testActivityID = "33333333-3333-4333-8333-333333333333"
)

// fakeUserLookup records the calls it received and returns a canned account.
type fakeUserLookup struct {
	result *identity.User
	err    error

	calls int
	gotID string
}

func (f *fakeUserLookup) UserByID(_ context.Context, id string) (*identity.User, error) {
	f.calls++
	f.gotID = id
	return f.result, f.err
}

// fakeActivityStore records the calls it received and returns canned rows.
type fakeActivityStore struct {
	result []Activity
	next   *Cursor
	err    error

	calls     int
	gotUserID string
	gotCursor *Cursor
	gotLimit  int
}

func (f *fakeActivityStore) ListForUser(_ context.Context, userID string, cursor *Cursor, limit int) ([]Activity, *Cursor, error) {
	f.calls++
	f.gotUserID = userID
	f.gotCursor = cursor
	f.gotLimit = limit
	return f.result, f.next, f.err
}

func newTestService(t *testing.T) (*Service, *fakeUserLookup, *fakeActivityStore) {
	t.Helper()

	users := &fakeUserLookup{result: &identity.User{ID: testUserID, DisplayName: "Ada Lovelace"}}
	activities := &fakeActivityStore{}

	service, err := NewService(users, activities)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, users, activities
}

// storyActivity is a well-formed story activity.
func storyActivity() Activity {
	return Activity{
		Kind:      KindStory,
		ID:        testActivityID,
		CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		Payload:   Payload{Title: "The first rain", Pillar: "wonder", Language: "en"},
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeActivityStore{}); err == nil {
		t.Error("NewService(nil users) error = nil, want an error")
	}
	if _, err := NewService(&fakeUserLookup{}, nil); err == nil {
		t.Error("NewService(nil activities) error = nil, want an error")
	}
}

func TestGetProfileReturnsUserAndPage(t *testing.T) {
	service, users, activities := newTestService(t)
	activities.result = []Activity{storyActivity()}

	got, next, err := service.GetProfile(context.Background(), testUserID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("GetProfile() error = %v, want nil", err)
	}

	if got.User == nil || got.User.ID != testUserID {
		t.Errorf("user = %+v, want the resolved owner", got.User)
	}
	if len(got.Activities) != 1 || got.Activities[0].Kind != KindStory {
		t.Errorf("activities = %+v, want one story", got.Activities)
	}
	if next != "" {
		t.Errorf("next cursor = %q, want empty on the last page", next)
	}
	if users.gotID != testUserID {
		t.Errorf("user lookup id = %q, want %q", users.gotID, testUserID)
	}
	if activities.gotLimit != DefaultListLimit || activities.gotCursor != nil {
		t.Errorf("store got limit %d cursor %+v, want the default limit and no cursor", activities.gotLimit, activities.gotCursor)
	}
}

func TestGetProfileEncodesNextCursor(t *testing.T) {
	service, _, activities := newTestService(t)
	next := NewCursor(storyActivity().CreatedAt, testActivityID)
	activities.next = &next

	_, gotNext, err := service.GetProfile(context.Background(), testUserID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("GetProfile() error = %v, want nil", err)
	}
	if gotNext == "" {
		t.Fatal("next cursor = empty, want the encoded cursor")
	}

	decoded, err := DecodeCursor(gotNext)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}
	if decoded.ID() != testActivityID || !decoded.CreatedAt().Equal(next.CreatedAt()) {
		t.Errorf("decoded cursor = %+v, want the store's next position", decoded)
	}
}

func TestGetProfileDecodesCursorForTheStore(t *testing.T) {
	service, _, activities := newTestService(t)
	raw := NewCursor(storyActivity().CreatedAt, testActivityID).Encode()

	if _, _, err := service.GetProfile(context.Background(), testUserID, raw, DefaultListLimit); err != nil {
		t.Fatalf("GetProfile() error = %v, want nil", err)
	}

	if activities.gotCursor == nil {
		t.Fatal("store cursor = nil, want the decoded cursor")
	}
	if activities.gotCursor.ID() != testActivityID {
		t.Errorf("store cursor id = %q, want %q", activities.gotCursor.ID(), testActivityID)
	}
}

func TestGetProfileMalformedUserIDIsNotFoundWithoutLookup(t *testing.T) {
	service, users, activities := newTestService(t)

	_, _, err := service.GetProfile(context.Background(), "not-a-uuid", "", DefaultListLimit)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if users.calls != 0 || activities.calls != 0 {
		t.Errorf("lookups = %d/%d, want none for a malformed id", users.calls, activities.calls)
	}
}

func TestGetProfileUnknownUserIsNotFound(t *testing.T) {
	service, users, activities := newTestService(t)
	users.err = identity.ErrUserNotFound

	_, _, err := service.GetProfile(context.Background(), testUserID, "", DefaultListLimit)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
	if activities.calls != 0 {
		t.Errorf("store calls = %d, want 0 when the owner does not exist", activities.calls)
	}
}

func TestGetProfileRejectsBadLimitWithoutQuerying(t *testing.T) {
	for _, limit := range []int{0, -1, MaxListLimit + 1} {
		service, users, activities := newTestService(t)

		_, _, err := service.GetProfile(context.Background(), testUserID, "", limit)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("limit %d: errors.Is(err, ErrValidation) = false, want true (err = %v)", limit, err)
		}
		if users.calls != 0 || activities.calls != 0 {
			t.Errorf("limit %d: lookups happened, want none", limit)
		}
	}
}

func TestGetProfileRejectsMalformedCursorWithoutQueryingTheStore(t *testing.T) {
	service, users, activities := newTestService(t)

	_, _, err := service.GetProfile(context.Background(), testUserID, "not-a-cursor", DefaultListLimit)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("errors.Is(err, ErrValidation) = false, want true (err = %v)", err)
	}
	if users.calls != 0 || activities.calls != 0 {
		t.Errorf("lookups = %d/%d, want none for a malformed cursor", users.calls, activities.calls)
	}
}

func TestGetProfileEmptyPageIsAnEmptySliceNotNil(t *testing.T) {
	service, _, activities := newTestService(t)
	activities.result = nil

	got, _, err := service.GetProfile(context.Background(), testUserID, "", DefaultListLimit)
	if err != nil {
		t.Fatalf("GetProfile() error = %v, want nil", err)
	}
	if got.Activities == nil {
		t.Error("activities = nil, want an empty slice")
	}
	if len(got.Activities) != 0 {
		t.Errorf("len(activities) = %d, want 0", len(got.Activities))
	}
}

func TestGetProfileStoreFailureIsWrapped(t *testing.T) {
	service, _, activities := newTestService(t)
	activities.err = errors.New("connection reset")

	_, _, err := service.GetProfile(context.Background(), testUserID, "", DefaultListLimit)
	if err == nil {
		t.Fatal("GetProfile() error = nil, want the store error")
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrValidation) {
		t.Errorf("err = %v, want an infrastructure error", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("err = %v, want it to wrap the store error", err)
	}
}

func TestGetProfileUserLookupFailureIsWrapped(t *testing.T) {
	service, users, _ := newTestService(t)
	users.err = errors.New("identity unavailable")

	_, _, err := service.GetProfile(context.Background(), testUserID, "", DefaultListLimit)
	if err == nil {
		t.Fatal("GetProfile() error = nil, want the lookup error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want an infrastructure error rather than not-found", err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 9, 12, 0, 0, 123456789, time.UTC)

	decoded, err := DecodeCursor(NewCursor(at, testActivityID).Encode())
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v, want nil", err)
	}
	if !decoded.CreatedAt().Equal(at) {
		t.Errorf("created at = %v, want %v", decoded.CreatedAt(), at)
	}
	if decoded.ID() != testActivityID {
		t.Errorf("id = %q, want %q", decoded.ID(), testActivityID)
	}
}

func TestDecodeCursorRejectsMalformedTokens(t *testing.T) {
	tests := map[string]string{
		"not base64":    "not a cursor!!",
		"no separator":  "MjAyNi0xMC0wOVQxMjowMDowMFo=",
		"bad timestamp": encodeRaw("not-a-time|" + testActivityID),
		"bad id":        encodeRaw("2026-10-09T12:00:00Z|not-a-uuid"),
	}

	for name, raw := range tests {
		if _, err := DecodeCursor(raw); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: errors.Is(err, ErrValidation) = false, want true (err = %v)", name, err)
		}
	}
}

// encodeRaw builds a cursor token from an arbitrary payload, so the decoder can be
// tested against payloads it must reject.
func encodeRaw(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}
