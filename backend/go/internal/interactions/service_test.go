package interactions

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recordingLogStore captures every record a Service writes, so a test can assert
// the weight that was applied without a database.
type recordingLogStore struct {
	records []Record
	err     error
}

func (s *recordingLogStore) Insert(_ context.Context, record Record) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, record)
	return nil
}

// newTestService returns a service over a fresh recording store.
func newTestService(t *testing.T) (*Service, *recordingLogStore) {
	t.Helper()

	store := &recordingLogStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}
	return service, store
}

// TestServiceLogAppliesTheWeightLadder is the contract the relevance engine
// depends on: each interaction type scores exactly what the ladder says.
func TestServiceLogAppliesTheWeightLadder(t *testing.T) {
	cases := []struct {
		interactionType string
		want            float64
	}{
		{interactionType: string(InteractionView), want: 1},
		{interactionType: string(InteractionReadFull), want: 3},
		{interactionType: string(InteractionReaction), want: 5},
		{interactionType: string(InteractionComment), want: 7},
		{interactionType: string(InteractionAdapt), want: 10},
	}

	for _, test := range cases {
		t.Run(test.interactionType, func(t *testing.T) {
			service, store := newTestService(t)

			if err := service.Log(
				context.Background(),
				"user-1",
				EntityStory,
				"0b6b0a5e-0000-4000-8000-000000000001",
				test.interactionType,
			); err != nil {
				t.Fatalf("Log() error = %v, want nil", err)
			}

			if len(store.records) != 1 {
				t.Fatalf("store received %d records, want 1", len(store.records))
			}
			record := store.records[0]
			if record.Type != InteractionType(test.interactionType) {
				t.Errorf("Type = %q, want %q", record.Type, test.interactionType)
			}
			if record.Weight != test.want {
				t.Errorf("Weight = %v, want %v", record.Weight, test.want)
			}
			if record.UserID != "user-1" || record.EntityType != EntityStory || record.EntityID == "" {
				t.Errorf("record = %+v, want the caller's ids", record)
			}
		})
	}
}

func TestServiceLogAcceptsBothEntityTypes(t *testing.T) {
	for _, entityType := range []string{EntityStory, EntityVersion} {
		t.Run(entityType, func(t *testing.T) {
			service, store := newTestService(t)

			if err := service.Log(
				context.Background(),
				"user-1",
				entityType,
				"0b6b0a5e-0000-4000-8000-000000000002",
				string(InteractionView),
			); err != nil {
				t.Fatalf("Log() error = %v, want nil", err)
			}
			if len(store.records) != 1 || store.records[0].EntityType != entityType {
				t.Errorf("records = %+v, want one %s row", store.records, entityType)
			}
		})
	}
}

// TestServiceLogRejectsInvalidInput proves that no weight is ever invented and
// no row is written for a caller's mistake.
func TestServiceLogRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name            string
		userID          string
		entityType      string
		entityID        string
		interactionType string
		wantMessage     string
	}{
		{
			name:            "unknown interaction type",
			userID:          "user-1",
			entityType:      EntityStory,
			entityID:        "0b6b0a5e-0000-4000-8000-000000000003",
			interactionType: "share",
			wantMessage:     "unknown interaction type",
		},
		{
			name:            "unknown entity type",
			userID:          "user-1",
			entityType:      "comment",
			entityID:        "0b6b0a5e-0000-4000-8000-000000000003",
			interactionType: string(InteractionView),
			wantMessage:     "unknown entity type",
		},
		{
			name:            "blank user",
			userID:          "   ",
			entityType:      EntityStory,
			entityID:        "0b6b0a5e-0000-4000-8000-000000000003",
			interactionType: string(InteractionView),
			wantMessage:     "requires a user id",
		},
		{
			name:            "blank entity",
			userID:          "user-1",
			entityType:      EntityVersion,
			entityID:        "",
			interactionType: string(InteractionView),
			wantMessage:     "requires an entity id",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, store := newTestService(t)

			err := service.Log(
				context.Background(),
				test.userID,
				test.entityType,
				test.entityID,
				test.interactionType,
			)
			if err == nil {
				t.Fatal("Log() error = nil, want a validation error")
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Log() error = %q, want it to contain %q", err, test.wantMessage)
			}
			if len(store.records) != 0 {
				t.Errorf("store received %d records, want 0", len(store.records))
			}
		})
	}
}

func TestServiceLogWrapsStoreFailures(t *testing.T) {
	store := &recordingLogStore{err: errors.New("connection reset")}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	logged := service.Log(
		context.Background(),
		"user-1",
		EntityStory,
		"0b6b0a5e-0000-4000-8000-000000000004",
		string(InteractionReaction),
	)
	if logged == nil {
		t.Fatal("Log() error = nil, want the store error")
	}
	if !errors.Is(logged, store.err) {
		t.Errorf("Log() error = %v, want it to wrap the store error", logged)
	}
}

func TestNewServiceRequiresAStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Fatal("NewService(nil) error = nil, want an error")
	}
}
