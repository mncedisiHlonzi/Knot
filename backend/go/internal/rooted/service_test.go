package rooted

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeStore is an in-memory RootedStore used to test the service without a
// database. It mimics the behaviour the service depends on: one primary signal per
// user, public filtering, and user existence.
type fakeStore struct {
	// signals is the rows per user, in insertion order.
	signals map[string][]Signal
	// users is the set of user ids that exist.
	users map[string]bool

	setErr    error
	listErr   error
	publicErr error
	batchErr  error
	existsErr error

	seq         int
	setCalls    int
	listCalls   int
	publicCalls int
	batchCalls  int
	gotSetInput Signal
	gotBatchIDs []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		signals: make(map[string][]Signal),
		users:   make(map[string]bool),
	}
}

func (s *fakeStore) SetPrimary(_ context.Context, userID string, signal Signal) (Signal, error) {
	s.setCalls++
	s.gotSetInput = signal
	if s.setErr != nil {
		return Signal{}, s.setErr
	}
	if !s.users[userID] {
		return Signal{}, ErrUserNotFound
	}

	s.seq++
	stored := signal
	stored.ID = "99999999-9999-4999-8999-999999999999"
	stored.UserID = userID

	// An upsert on is_primary: replace the existing primary row if there is one.
	rows := s.signals[userID]
	replaced := false
	for i := range rows {
		if rows[i].IsPrimary {
			rows[i] = stored
			replaced = true
			break
		}
	}
	if !replaced {
		rows = append(rows, stored)
	}
	s.signals[userID] = rows

	return stored, nil
}

func (s *fakeStore) ListByUser(_ context.Context, userID string) ([]Signal, error) {
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.signals[userID], nil
}

func (s *fakeStore) ListPublicByUser(_ context.Context, userID string) ([]Signal, error) {
	s.publicCalls++
	if s.publicErr != nil {
		return nil, s.publicErr
	}

	out := make([]Signal, 0, len(s.signals[userID]))
	for _, signal := range s.signals[userID] {
		if signal.IsPublic {
			out = append(out, signal)
		}
	}
	return out, nil
}

func (s *fakeStore) BatchPrimaryPublic(_ context.Context, userIDs []string) (map[string]Signal, error) {
	s.batchCalls++
	s.gotBatchIDs = append(s.gotBatchIDs, userIDs...)
	if s.batchErr != nil {
		return nil, s.batchErr
	}

	out := make(map[string]Signal, len(userIDs))
	for _, userID := range userIDs {
		for _, signal := range s.signals[userID] {
			if signal.IsPrimary && signal.IsPublic {
				out[userID] = signal
			}
		}
	}
	return out, nil
}

func (s *fakeStore) UserExists(_ context.Context, userID string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.users[userID], nil
}

// seed inserts a signal directly, as earlier requests would have left it, and
// registers the owner as an existing user.
func (s *fakeStore) seed(signal Signal) {
	s.users[signal.UserID] = true
	s.signals[signal.UserID] = append(s.signals[signal.UserID], signal)
}

// seedUser registers a user that has no signals yet.
func (s *fakeStore) seedUser(userID string) {
	s.users[userID] = true
}

const (
	userOne = "11111111-1111-4111-8111-111111111111"
	userTwo = "22222222-2222-4222-8222-222222222222"
)

func newTestService(t *testing.T, store RootedStore) *Service {
	t.Helper()

	service, err := NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}
	return service
}

func validInput() SetSignalInput {
	return SetSignalInput{
		Place:          "  Cape Town  ",
		DurationBucket: DurationLifelong,
		IsPublic:       true,
	}
}

func TestNewServiceRejectsNilStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Error("NewService(nil) error = nil, want an error")
	}
}

func TestSetSignalTrimsAndStoresPrimary(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	stored, err := service.SetSignal(context.Background(), userOne, validInput())
	if err != nil {
		t.Fatalf("SetSignal() error = %v, want nil", err)
	}

	if stored.Place != "Cape Town" {
		t.Errorf("place = %q, want it trimmed", stored.Place)
	}
	if !stored.IsPrimary {
		t.Error("is_primary = false, want every stored signal to be the primary one")
	}
	if !store.gotSetInput.IsPrimary {
		t.Error("store received is_primary = false, want true")
	}
	if store.setCalls != 1 {
		t.Errorf("store received %d set calls, want 1", store.setCalls)
	}
}

func TestSetSignalReplacesThePrimary(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	if _, err := service.SetSignal(context.Background(), userOne, SetSignalInput{Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true}); err != nil {
		t.Fatalf("first SetSignal() error = %v, want nil", err)
	}
	if _, err := service.SetSignal(context.Background(), userOne, SetSignalInput{Place: "Johannesburg", DurationBucket: DurationManyYears, IsPublic: true}); err != nil {
		t.Fatalf("second SetSignal() error = %v, want nil", err)
	}

	signals, err := service.GetMySignals(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetMySignals() error = %v, want nil", err)
	}

	if len(signals) != 1 {
		t.Fatalf("signals = %d, want exactly 1 primary after a replacement", len(signals))
	}
	if signals[0].Place != "Johannesburg" {
		t.Errorf("place = %q, want the replacement", signals[0].Place)
	}
}

func TestSetSignalRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input SetSignalInput
		field string
	}{
		{name: "empty place", input: SetSignalInput{Place: "", DurationBucket: DurationLifelong}, field: "place"},
		{name: "blank place", input: SetSignalInput{Place: "   ", DurationBucket: DurationLifelong}, field: "place"},
		{name: "newline in place", input: SetSignalInput{Place: "Cape\nTown", DurationBucket: DurationLifelong}, field: "place"},
		{name: "carriage return in place", input: SetSignalInput{Place: "Cape\rTown", DurationBucket: DurationLifelong}, field: "place"},
		{name: "tab in place", input: SetSignalInput{Place: "Cape\tTown", DurationBucket: DurationLifelong}, field: "place"},
		{name: "too long", input: SetSignalInput{Place: strings.Repeat("a", MaxPlaceLength+1), DurationBucket: DurationLifelong}, field: "place"},
		{name: "invalid bucket", input: SetSignalInput{Place: "Cape Town", DurationBucket: "years"}, field: "duration_bucket"},
		{name: "empty bucket", input: SetSignalInput{Place: "Cape Town", DurationBucket: ""}, field: "duration_bucket"},
		{name: "latitude without longitude", input: SetSignalInput{Place: "Cape Town", DurationBucket: DurationLifelong, Latitude: floatPtr(1.5)}, field: "latitude"},
		{name: "longitude without latitude", input: SetSignalInput{Place: "Cape Town", DurationBucket: DurationLifelong, Longitude: floatPtr(1.5)}, field: "latitude"},
		{name: "latitude out of range", input: SetSignalInput{Place: "Cape Town", DurationBucket: DurationLifelong, Latitude: floatPtr(-91), Longitude: floatPtr(0)}, field: "latitude"},
		{name: "longitude out of range", input: SetSignalInput{Place: "Cape Town", DurationBucket: DurationLifelong, Latitude: floatPtr(0), Longitude: floatPtr(200)}, field: "longitude"},
		{name: "country too long", input: SetSignalInput{Place: "Cape Town", DurationBucket: DurationLifelong, PlaceCountry: strings.Repeat("c", MaxPlaceCountryLength+1)}, field: "place_country"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeStore()
			store.seedUser(userOne)
			service := newTestService(t, store)

			_, err := service.SetSignal(context.Background(), userOne, test.input)

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("SetSignal() error = %v, want *ValidationError", err)
			}
			if validation.Field != test.field {
				t.Errorf("validation field = %q, want %q", validation.Field, test.field)
			}
			if !errors.Is(err, ErrValidation) {
				t.Error("errors.Is(err, ErrValidation) = false, want true")
			}
			if store.setCalls != 0 {
				t.Errorf("store received %d calls, want 0 — invalid input must not reach the store", store.setCalls)
			}
		})
	}
}

// floatPtr returns a pointer to v, for the optional coordinate fields.
func floatPtr(v float64) *float64 { return &v }

func TestSetSignalStoresCoordinates(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	stored, err := service.SetSignal(context.Background(), userOne, SetSignalInput{
		Place:          "Manguzi",
		Latitude:       floatPtr(-26.9998),
		Longitude:      floatPtr(32.7489),
		PlaceCountry:   " South Africa ",
		DurationBucket: DurationLifelong,
	})
	if err != nil {
		t.Fatalf("SetSignal() error = %v, want nil", err)
	}

	if store.gotSetInput.Latitude == nil || *store.gotSetInput.Latitude != -26.9998 {
		t.Errorf("latitude = %v, want -26.9998", store.gotSetInput.Latitude)
	}
	if store.gotSetInput.Longitude == nil || *store.gotSetInput.Longitude != 32.7489 {
		t.Errorf("longitude = %v, want 32.7489", store.gotSetInput.Longitude)
	}
	if stored.PlaceCountry == nil || *stored.PlaceCountry != "South Africa" {
		t.Errorf("place country = %v, want the trimmed value", stored.PlaceCountry)
	}
}

func TestSetSignalWithoutCoordinatesLeavesThemNil(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	if _, err := service.SetSignal(context.Background(), userOne, validInput()); err != nil {
		t.Fatalf("SetSignal() error = %v, want nil", err)
	}
	if store.gotSetInput.Latitude != nil || store.gotSetInput.Longitude != nil {
		t.Errorf("coordinates = (%v, %v), want nil", store.gotSetInput.Latitude, store.gotSetInput.Longitude)
	}
}

func TestSetSignalAcceptsMaxLengthPlace(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	if _, err := service.SetSignal(context.Background(), userOne, SetSignalInput{
		Place:          strings.Repeat("a", MaxPlaceLength),
		DurationBucket: DurationLifelong,
	}); err != nil {
		t.Fatalf("SetSignal() error = %v, want nil for an exactly-maximal place", err)
	}
}

func TestSetSignalRejectsMalformedUserID(t *testing.T) {
	service := newTestService(t, newFakeStore())

	_, err := service.SetSignal(context.Background(), "not-a-uuid", validInput())
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetSignal() error = %v, want ErrUserNotFound", err)
	}
}

func TestSetSignalMapsStoreUserNotFound(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store)

	// The user was never registered, so the fake store reports not-found.
	_, err := service.SetSignal(context.Background(), userOne, validInput())
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetSignal() error = %v, want ErrUserNotFound", err)
	}
}

func TestGetMySignalsReturnsPublicAndPrivate(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	store.seed(Signal{UserID: userOne, Place: "Durban", DurationBucket: DurationRecently, IsPublic: false, IsPrimary: false})
	service := newTestService(t, store)

	signals, err := service.GetMySignals(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetMySignals() error = %v, want nil", err)
	}

	if len(signals) != 2 {
		t.Errorf("signals = %d, want 2 (the owner sees private signals too)", len(signals))
	}
}

func TestGetMySignalsReturnsEmptySliceNotNull(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	signals, err := service.GetMySignals(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetMySignals() error = %v, want nil", err)
	}
	if signals == nil {
		t.Error("signals = nil, want an empty slice rather than nil")
	}
}

func TestGetMySignalsRejectsMalformedUserID(t *testing.T) {
	service := newTestService(t, newFakeStore())

	if _, err := service.GetMySignals(context.Background(), "nope"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetMySignals() error = %v, want ErrUserNotFound", err)
	}
}

func TestGetPublicSignalsFiltersPrivate(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	store.seed(Signal{UserID: userOne, Place: "Durban", DurationBucket: DurationRecently, IsPublic: false})
	service := newTestService(t, store)

	signals, err := service.GetPublicSignals(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetPublicSignals() error = %v, want nil", err)
	}

	if len(signals) != 1 {
		t.Fatalf("signals = %d, want only the public signal", len(signals))
	}
	if signals[0].Place != "Cape Town" {
		t.Errorf("place = %q, want %q", signals[0].Place, "Cape Town")
	}
}

func TestGetPublicSignalsUserNotFound(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store)

	if _, err := service.GetPublicSignals(context.Background(), userOne); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetPublicSignals() error = %v, want ErrUserNotFound for an unknown user", err)
	}
}

func TestGetPublicSignalsRejectsMalformedUserID(t *testing.T) {
	service := newTestService(t, newFakeStore())

	if _, err := service.GetPublicSignals(context.Background(), "not-a-uuid"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetPublicSignals() error = %v, want ErrUserNotFound", err)
	}
}

func TestGetPrimaryPublicSignalReturnsNilWhenNone(t *testing.T) {
	store := newFakeStore()
	store.seedUser(userOne)
	service := newTestService(t, store)

	signal, err := service.GetPrimaryPublicSignal(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetPrimaryPublicSignal() error = %v, want nil", err)
	}
	if signal != nil {
		t.Errorf("signal = %+v, want nil when the user has no public signal", signal)
	}
}

func TestGetPrimaryPublicSignalHidesPrivate(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: false, IsPrimary: true})
	service := newTestService(t, store)

	signal, err := service.GetPrimaryPublicSignal(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetPrimaryPublicSignal() error = %v, want nil", err)
	}
	if signal != nil {
		t.Errorf("signal = %+v, want nil for a hidden signal", signal)
	}
}

func TestGetPrimaryPublicSignalReturnsTheSignal(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	service := newTestService(t, store)

	signal, err := service.GetPrimaryPublicSignal(context.Background(), userOne)
	if err != nil {
		t.Fatalf("GetPrimaryPublicSignal() error = %v, want nil", err)
	}
	if signal == nil {
		t.Fatal("signal = nil, want the primary public signal")
	}
	if signal.Place != "Cape Town" {
		t.Errorf("place = %q, want %q", signal.Place, "Cape Town")
	}
}

func TestBatchGetPrimaryPublicSignalsEmptyInputSkipsTheStore(t *testing.T) {
	store := newFakeStore()
	service := newTestService(t, store)

	result, err := service.BatchGetPrimaryPublicSignals(context.Background(), []string{})
	if err != nil {
		t.Fatalf("BatchGetPrimaryPublicSignals() error = %v, want nil", err)
	}
	if result == nil {
		t.Error("result = nil, want an empty map rather than nil")
	}
	if len(result) != 0 {
		t.Errorf("result = %+v, want empty", result)
	}
	if store.batchCalls != 0 {
		t.Errorf("store received %d batch calls, want 0 for empty input", store.batchCalls)
	}
}

func TestBatchGetPrimaryPublicSignalsSingle(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	service := newTestService(t, store)

	result, err := service.BatchGetPrimaryPublicSignals(context.Background(), []string{userOne})
	if err != nil {
		t.Fatalf("BatchGetPrimaryPublicSignals() error = %v, want nil", err)
	}
	if len(result) != 1 {
		t.Fatalf("result = %d entries, want 1", len(result))
	}
	if result[userOne] == nil || result[userOne].Place != "Cape Town" {
		t.Errorf("result[userOne] = %+v, want the signal", result[userOne])
	}
	if store.batchCalls != 1 {
		t.Errorf("store received %d batch calls, want 1", store.batchCalls)
	}
}

func TestBatchGetPrimaryPublicSignalsMultiple(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	store.seed(Signal{UserID: userTwo, Place: "Durban", DurationBucket: DurationRecently, IsPublic: true, IsPrimary: true})
	service := newTestService(t, store)

	result, err := service.BatchGetPrimaryPublicSignals(context.Background(), []string{userOne, userTwo})
	if err != nil {
		t.Fatalf("BatchGetPrimaryPublicSignals() error = %v, want nil", err)
	}
	if len(result) != 2 {
		t.Fatalf("result = %d entries, want 2", len(result))
	}
	if store.batchCalls != 1 {
		t.Errorf("store received %d batch calls, want exactly 1", store.batchCalls)
	}
	if len(store.gotBatchIDs) != 2 {
		t.Errorf("store received ids %v, want both authors", store.gotBatchIDs)
	}
}

func TestBatchGetPrimaryPublicSignalsMissingUsersAreAbsent(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	service := newTestService(t, store)

	result, err := service.BatchGetPrimaryPublicSignals(context.Background(), []string{userOne, userTwo})
	if err != nil {
		t.Fatalf("BatchGetPrimaryPublicSignals() error = %v, want nil", err)
	}
	if len(result) != 1 {
		t.Fatalf("result = %d entries, want 1", len(result))
	}
	if _, ok := result[userTwo]; ok {
		t.Error("result contains a user with no signal, want them absent")
	}
}

func TestBatchGetPrimaryPublicSignalsDropsMalformedIDs(t *testing.T) {
	store := newFakeStore()
	store.seed(Signal{UserID: userOne, Place: "Cape Town", DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true})
	service := newTestService(t, store)

	result, err := service.BatchGetPrimaryPublicSignals(context.Background(), []string{"", "not-a-uuid", userOne, userOne})
	if err != nil {
		t.Fatalf("BatchGetPrimaryPublicSignals() error = %v, want nil", err)
	}
	if len(result) != 1 {
		t.Fatalf("result = %d entries, want 1", len(result))
	}
	if len(store.gotBatchIDs) != 1 {
		t.Errorf("store received ids %v, want only the one canonical, distinct UUID", store.gotBatchIDs)
	}
}

func TestBatchGetPrimaryPublicSignalsPropagatesStoreErrors(t *testing.T) {
	store := newFakeStore()
	store.batchErr = errors.New("boom")
	service := newTestService(t, store)

	if _, err := service.BatchGetPrimaryPublicSignals(context.Background(), []string{userOne}); err == nil {
		t.Error("BatchGetPrimaryPublicSignals() error = nil, want the store error")
	}
}
