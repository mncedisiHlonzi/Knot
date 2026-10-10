package reactions

import (
	"context"
	"errors"
	"testing"

	"github.com/knot/backend/internal/identity"
)

// Canonical UUID fixtures.
const (
	userA   = "11111111-1111-4111-8111-111111111111"
	userB   = "22222222-2222-4222-8222-222222222222"
	entityX = "33333333-3333-4333-8333-333333333333"
	entityY = "44444444-4444-4444-8444-444444444444"
)

// fakeStore is an in-memory ReactionStore that records what it was asked for.
type fakeStore struct {
	active      bool
	toggleErr   error
	gotToggle   Reaction
	toggleCalls int

	list    []Reaction
	listErr error

	summaries     map[string]Summary
	summariesErr  error
	gotSummaryIDs []string

	mine          map[string][]ReactionType
	mineErr       error
	gotMineUserID string
	gotMineIDs    []string
}

func (f *fakeStore) Toggle(_ context.Context, reaction Reaction) (bool, error) {
	f.toggleCalls++
	f.gotToggle = reaction
	return f.active, f.toggleErr
}

func (f *fakeStore) ListForEntity(_ context.Context, _ EntityType, _ string) ([]Reaction, error) {
	return f.list, f.listErr
}

func (f *fakeStore) Summaries(_ context.Context, _ EntityType, entityIDs []string) (map[string]Summary, error) {
	f.gotSummaryIDs = entityIDs
	return f.summaries, f.summariesErr
}

func (f *fakeStore) MyReactions(_ context.Context, userID string, _ EntityType, entityIDs []string) (map[string][]ReactionType, error) {
	f.gotMineUserID = userID
	f.gotMineIDs = entityIDs
	return f.mine, f.mineErr
}

// fakeActors is the batched identity lookup.
type fakeActors struct {
	users  map[string]*identity.User
	err    error
	calls  int
	gotIDs []string
}

func (f *fakeActors) UsersByIDs(_ context.Context, ids []string) (map[string]*identity.User, error) {
	f.calls++
	f.gotIDs = ids
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

// discardLogger swallows the enrichment warnings these tests provoke.
type discardLogger struct{}

func (discardLogger) WarnContext(context.Context, string, ...any) {}

func newTestService(t *testing.T, store ReactionStore, actors ActorLookup) *Service {
	t.Helper()

	service, err := NewService(store, actors, discardLogger{})
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}
	return service
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, &fakeActors{}, discardLogger{}); err == nil {
		t.Error("NewService(nil store) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, nil, discardLogger{}); err == nil {
		t.Error("NewService(nil actors) error = nil, want an error")
	}
	if _, err := NewService(&fakeStore{}, &fakeActors{}, nil); err == nil {
		t.Error("NewService(nil logger) error = nil, want an error")
	}
}

func TestToggleReportsActiveWhenCreated(t *testing.T) {
	store := &fakeStore{active: true, summaries: map[string]Summary{entityX: {RingsTrue: 1}}}
	service := newTestService(t, store, &fakeActors{})

	summary, active, err := service.Toggle(context.Background(), ToggleInput{
		UserID:       userA,
		EntityType:   EntityStory,
		EntityID:     entityX,
		ReactionType: RingsTrue,
	})
	if err != nil {
		t.Fatalf("Toggle() error = %v, want nil", err)
	}
	if !active {
		t.Error("Toggle() active = false, want true when the reaction was created")
	}
	if summary.RingsTrue != 1 {
		t.Errorf("summary.rings_true = %d, want 1", summary.RingsTrue)
	}
	if store.gotToggle.UserID != userA || store.gotToggle.ReactionType != RingsTrue {
		t.Errorf("stored reaction = %+v, want user A / rings_true", store.gotToggle)
	}
}

func TestToggleReportsInactiveWhenRemoved(t *testing.T) {
	store := &fakeStore{active: false}
	service := newTestService(t, store, &fakeActors{})

	_, active, err := service.Toggle(context.Background(), ToggleInput{
		UserID:       userA,
		EntityType:   EntityStory,
		EntityID:     entityX,
		ReactionType: RingsTrue,
	})
	if err != nil {
		t.Fatalf("Toggle() error = %v, want nil", err)
	}
	if active {
		t.Error("Toggle() active = true, want false when the reaction was removed")
	}
}

func TestToggleRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input ToggleInput
	}{
		{"bad user id", ToggleInput{UserID: "nope", EntityType: EntityStory, EntityID: entityX, ReactionType: RingsTrue}},
		{"bad entity type", ToggleInput{UserID: userA, EntityType: "profile", EntityID: entityX, ReactionType: RingsTrue}},
		{"bad entity id", ToggleInput{UserID: userA, EntityType: EntityStory, EntityID: "nope", ReactionType: RingsTrue}},
		{"bad reaction type", ToggleInput{UserID: userA, EntityType: EntityStory, EntityID: entityX, ReactionType: "like"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{}
			service := newTestService(t, store, &fakeActors{})

			if _, _, err := service.Toggle(context.Background(), test.input); !errors.Is(err, ErrValidation) {
				t.Errorf("Toggle() error = %v, want ErrValidation", err)
			}
			if store.toggleCalls != 0 {
				t.Errorf("store.Toggle calls = %d, want 0 for invalid input", store.toggleCalls)
			}
		})
	}
}

func TestListForEntityEnrichesActorsInOneCall(t *testing.T) {
	store := &fakeStore{list: []Reaction{
		{ID: entityX, UserID: userA, ReactionType: RingsTrue},
		{ID: entityY, UserID: userB, ReactionType: AddsSomethingNew},
	}}
	actors := &fakeActors{users: map[string]*identity.User{
		userA: {ID: userA, DisplayName: "Ada Lovelace", AvatarURL: "avatars/" + userA + "/ada.png"},
		userB: {ID: userB, DisplayName: "Grace Hopper"},
	}}
	service := newTestService(t, store, actors)

	list, err := service.ListForEntity(context.Background(), EntityStory, entityX)
	if err != nil {
		t.Fatalf("ListForEntity() error = %v, want nil", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	if list[0].Actor.DisplayName != "Ada Lovelace" {
		t.Errorf("actor[0].DisplayName = %q, want Ada Lovelace", list[0].Actor.DisplayName)
	}
	if list[0].Actor.AvatarURL != "/users/"+userA+"/avatar?v=ada.png" {
		t.Errorf("actor[0].AvatarURL = %q, want the backend avatar path", list[0].Actor.AvatarURL)
	}
	// Grace has no avatar, so the path is empty.
	if list[1].Actor.AvatarURL != "" {
		t.Errorf("actor[1].AvatarURL = %q, want empty for no avatar", list[1].Actor.AvatarURL)
	}
	if actors.calls != 1 {
		t.Errorf("actor lookup calls = %d, want 1 batched call", actors.calls)
	}
}

func TestListForEntitySwallowsActorLookupFailure(t *testing.T) {
	store := &fakeStore{list: []Reaction{{ID: entityX, UserID: userA, ReactionType: RingsTrue}}}
	actors := &fakeActors{err: errors.New("identity down")}
	service := newTestService(t, store, actors)

	list, err := service.ListForEntity(context.Background(), EntityStory, entityX)
	if err != nil {
		t.Fatalf("ListForEntity() error = %v, want nil (enrichment is supplementary)", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	// The actor is unresolved but the id is still reported.
	if list[0].Actor.ID != userA {
		t.Errorf("actor id = %q, want %q", list[0].Actor.ID, userA)
	}
}

func TestListForEntityRejectsBadInput(t *testing.T) {
	service := newTestService(t, &fakeStore{}, &fakeActors{})

	if _, err := service.ListForEntity(context.Background(), "profile", entityX); !errors.Is(err, ErrValidation) {
		t.Errorf("bad entity type error = %v, want ErrValidation", err)
	}
	if _, err := service.ListForEntity(context.Background(), EntityStory, "nope"); !errors.Is(err, ErrValidation) {
		t.Errorf("bad entity id error = %v, want ErrValidation", err)
	}
}

func TestSummaryForEntity(t *testing.T) {
	store := &fakeStore{summaries: map[string]Summary{entityX: {RingsTrue: 3, NeedsASource: 1}}}
	service := newTestService(t, store, &fakeActors{})

	summary, err := service.SummaryForEntity(context.Background(), EntityStory, entityX)
	if err != nil {
		t.Fatalf("SummaryForEntity() error = %v, want nil", err)
	}
	if summary.RingsTrue != 3 || summary.NeedsASource != 1 {
		t.Errorf("summary = %+v, want rings_true 3 / needs_a_source 1", summary)
	}
}

func TestSummaryForEntityMissingIsZero(t *testing.T) {
	store := &fakeStore{summaries: map[string]Summary{}}
	service := newTestService(t, store, &fakeActors{})

	summary, err := service.SummaryForEntity(context.Background(), EntityStory, entityX)
	if err != nil {
		t.Fatalf("SummaryForEntity() error = %v, want nil", err)
	}
	if summary.Total() != 0 {
		t.Errorf("summary.Total() = %d, want 0", summary.Total())
	}
}

func TestBatchSummariesEmptyReturnsEmptyMap(t *testing.T) {
	service := newTestService(t, &fakeStore{}, &fakeActors{})

	got, err := service.BatchSummaries(context.Background(), EntityStory, nil)
	if err != nil {
		t.Fatalf("BatchSummaries() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("len(BatchSummaries()) = %d, want 0", len(got))
	}
}

func TestBatchSummariesDropsInvalidIds(t *testing.T) {
	store := &fakeStore{summaries: map[string]Summary{entityX: {RingsTrue: 2}}}
	service := newTestService(t, store, &fakeActors{})

	got, err := service.BatchSummaries(context.Background(), EntityStory, []string{entityX, "not-a-uuid"})
	if err != nil {
		t.Fatalf("BatchSummaries() error = %v, want nil", err)
	}
	if len(store.gotSummaryIDs) != 1 || store.gotSummaryIDs[0] != entityX {
		t.Errorf("store ids = %v, want [%s]", store.gotSummaryIDs, entityX)
	}
	if got[entityX].RingsTrue != 2 {
		t.Errorf("summary[entityX].RingsTrue = %d, want 2", got[entityX].RingsTrue)
	}
}

func TestBatchSummariesRejectsInvalidEntityType(t *testing.T) {
	service := newTestService(t, &fakeStore{}, &fakeActors{})

	if _, err := service.BatchSummaries(context.Background(), "profile", []string{entityX}); !errors.Is(err, ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

func TestBatchMyReactionsSkipsStoreForAnonymous(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, &fakeActors{})

	got, err := service.BatchMyReactions(context.Background(), "", EntityStory, []string{entityX})
	if err != nil {
		t.Fatalf("BatchMyReactions() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
	if store.gotMineUserID != "" {
		t.Errorf("store was called for an anonymous reader (user id %q)", store.gotMineUserID)
	}
}

func TestBatchMyReactionsReturnsSignals(t *testing.T) {
	store := &fakeStore{mine: map[string][]ReactionType{entityX: {RingsTrue, NeedsASource}}}
	service := newTestService(t, store, &fakeActors{})

	got, err := service.BatchMyReactions(context.Background(), userA, EntityStory, []string{entityX})
	if err != nil {
		t.Fatalf("BatchMyReactions() error = %v, want nil", err)
	}
	if len(got[entityX]) != 2 {
		t.Fatalf("got[entityX] = %v, want two signals", got[entityX])
	}
	if store.gotMineUserID != userA {
		t.Errorf("store user id = %q, want %q", store.gotMineUserID, userA)
	}
}

func TestUniqueIDsDeduplicates(t *testing.T) {
	got := uniqueIDs([]string{"a", "", "a", "b", "b", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("uniqueIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("uniqueIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIsUUID(t *testing.T) {
	if !isUUID("11111111-1111-4111-8111-111111111111") {
		t.Error("isUUID(valid) = false, want true")
	}
	for _, invalid := range []string{"", "nope", "11111111-1111-4111-8111-11111111111", "11111111-1111-4111-8111-1111111111111"} {
		if isUUID(invalid) {
			t.Errorf("isUUID(%q) = true, want false", invalid)
		}
	}
}
